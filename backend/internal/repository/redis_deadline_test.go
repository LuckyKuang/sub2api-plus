//go:build unit || !integration

package repository

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestBuildRedisOptionsPreservesCallerDeadlinesForPlaintextAndTLS(t *testing.T) {
	for _, tlsEnabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("tls=%t", tlsEnabled), func(t *testing.T) {
			cfg := &config.Config{Redis: config.RedisConfig{Host: "redis.internal", Port: 6379, EnableTLS: tlsEnabled,
				ReadTimeoutSeconds: 30, WriteTimeoutSeconds: 30}}
			opts := buildRedisOptions(cfg)
			require.True(t, opts.ContextTimeoutEnabled, "request and lease deadlines must bound Redis socket I/O even when deployment timeouts are longer")
			client := InitRedis(cfg)
			t.Cleanup(func() { _ = client.Close() })
			require.True(t, client.Options().ContextTimeoutEnabled, "the runtime client must retain the same deadline behavior")
			if tlsEnabled {
				require.NotNil(t, opts.TLSConfig)
				require.Equal(t, "redis.internal", opts.TLSConfig.ServerName)
			} else {
				require.Nil(t, opts.TLSConfig)
			}
		})
	}
}

// A real TCP peer completes go-redis's normal handshake, then either leaves a
// command unanswered or stops reading after PING. A fake cache which observes
// ctx.Err cannot detect a client that discards the context before socket I/O.
type redisDeadlineResponder struct {
	listener    net.Listener
	stop        chan struct{}
	paused      chan struct{}
	commands    chan string
	writeStall  bool
	mu          sync.Mutex
	connections map[net.Conn]struct{}
	wg          sync.WaitGroup
	pausedOnce  sync.Once
}

func newRedisDeadlineResponder(t *testing.T, writeStall bool) (*redisDeadlineResponder, *config.Config) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := &redisDeadlineResponder{listener: listener, stop: make(chan struct{}), paused: make(chan struct{}),
		commands: make(chan string, 8), writeStall: writeStall, connections: make(map[net.Conn]struct{})}
	server.wg.Add(1)
	go func() {
		defer server.wg.Done()
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			if tcp, ok := conn.(*net.TCPConn); ok {
				_ = tcp.SetReadBuffer(4096)
			}
			server.mu.Lock()
			select {
			case <-server.stop:
				server.mu.Unlock()
				_ = conn.Close()
				return
			default:
			}
			server.connections[conn] = struct{}{}
			server.wg.Add(1)
			server.mu.Unlock()
			go server.serve(conn)
		}
	}()
	t.Cleanup(func() {
		close(server.stop)
		_ = listener.Close()
		server.mu.Lock()
		for conn := range server.connections {
			_ = conn.Close()
		}
		server.mu.Unlock()
		server.wg.Wait()
	})
	addr, ok := listener.Addr().(*net.TCPAddr)
	require.True(t, ok)
	cfg := &config.Config{Redis: config.RedisConfig{Host: "127.0.0.1", Port: addr.Port,
		DialTimeoutSeconds: 1, ReadTimeoutSeconds: 4, WriteTimeoutSeconds: 4, PoolSize: 4}}
	return server, cfg
}

func (s *redisDeadlineResponder) serve(conn net.Conn) {
	defer s.wg.Done()
	defer func() { _ = conn.Close(); s.mu.Lock(); delete(s.connections, conn); s.mu.Unlock() }()
	reader := bufio.NewReader(conn)
	for {
		command, err := readDeadlineRedisCommand(reader)
		if err != nil || len(command) == 0 {
			return
		}
		name := strings.ToUpper(command[0])
		switch name {
		case "HELLO":
			// The supported Redis RESP2 fallback avoids fixture dependence on
			// optional RESP3 maintenance notifications or identity commands.
			if _, err = io.WriteString(conn, "-ERR unknown command 'hello'\r\n"); err != nil {
				return
			}
		case "PING":
			if _, err = io.WriteString(conn, "+PONG\r\n"); err != nil {
				return
			}
			if s.writeStall {
				s.pausedOnce.Do(func() { close(s.paused) })
				<-s.stop
				return
			}
		case "GET", "EVALSHA", "EVAL", "ZREM":
			s.commands <- name
			<-s.stop
			return
		default:
			if _, err = io.WriteString(conn, "+OK\r\n"); err != nil {
				return
			}
		}
	}
}

func readDeadlineRedisCommand(reader *bufio.Reader) ([]string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	if len(line) < 3 || line[0] != '*' {
		return nil, fmt.Errorf("invalid test RESP command")
	}
	count, err := strconv.Atoi(strings.TrimSpace(line[1:]))
	if err != nil || count < 1 || count > 128 {
		return nil, fmt.Errorf("invalid test RESP argument count")
	}
	command := make([]string, count)
	for i := range command {
		line, err = reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		if len(line) < 3 || line[0] != '$' {
			return nil, fmt.Errorf("invalid test RESP argument")
		}
		size, parseErr := strconv.Atoi(strings.TrimSpace(line[1:]))
		if parseErr != nil || size < 0 || size > 1<<20 {
			return nil, fmt.Errorf("invalid test RESP argument length")
		}
		payload := make([]byte, size+2)
		if _, err = io.ReadFull(reader, payload); err != nil {
			return nil, err
		}
		command[i] = string(payload[:size])
	}
	return command, nil
}

func TestRedisCallerDeadlineBoundsSlowNetworkRead(t *testing.T) {
	server, cfg := newRedisDeadlineResponder(t, false)
	client := InitRedis(cfg)
	t.Cleanup(func() { _ = client.Close() })
	require.NoError(t, client.Ping(context.Background()).Err())
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := client.Get(ctx, "user-capacity").Err()
	elapsed := time.Since(started)
	require.Error(t, err)
	require.Less(t, elapsed, 500*time.Millisecond, "a slow Redis read must honor the caller's deadline instead of the four-second deployment timeout")
	select {
	case command := <-server.commands:
		require.Equal(t, "GET", command)
	default:
		t.Fatal("test did not reach the real Redis network read")
	}
}

type redisDeadlineWriteObserver struct {
	failures chan error
}

func (h redisDeadlineWriteObserver) DialHook(next redis.DialHook) redis.DialHook {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := next(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		return redisDeadlineObservedConn{Conn: conn, failures: h.failures}, nil
	}
}

func (h redisDeadlineWriteObserver) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return next
}
func (h redisDeadlineWriteObserver) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

type redisDeadlineObservedConn struct {
	net.Conn
	failures chan error
}

func (c redisDeadlineObservedConn) Write(payload []byte) (int, error) {
	written, err := c.Conn.Write(payload)
	if err != nil {
		select {
		case c.failures <- err:
		default:
		}
	}
	return written, err
}

func TestRedisCallerDeadlineBoundsBlockedNetworkWrite(t *testing.T) {
	server, cfg := newRedisDeadlineResponder(t, true)
	client := InitRedis(cfg)
	t.Cleanup(func() { _ = client.Close() })
	writeFailures := make(chan error, 1)
	client.AddHook(redisDeadlineWriteObserver{failures: writeFailures})
	require.NoError(t, client.Ping(context.Background()).Err())
	<-server.paused
	// The peer no longer reads and advertises a small receive buffer. This
	// payload exceeds TCP send capacity, so the write itself must stop on ctx.
	payload := strings.Repeat("x", 16<<20)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := client.Set(ctx, "pending-capacity", payload, time.Minute).Err()
	elapsed := time.Since(started)
	require.Error(t, err)
	require.Less(t, elapsed, 500*time.Millisecond, "a blocked Redis write must not pin an execution until the deployment timeout")
	select {
	case writeErr := <-writeFailures:
		var timeout net.Error
		require.ErrorAs(t, writeErr, &timeout)
		require.True(t, timeout.Timeout(), "the real TCP Write must stop on its deadline before any read can happen")
	default:
		t.Fatal("the write completed; this scenario did not exercise Redis network-write backpressure")
	}
}

func TestRedisConcurrencyOperationsRespectTwoSecondBudget(t *testing.T) {
	server, cfg := newRedisDeadlineResponder(t, false)
	client := InitRedis(cfg)
	t.Cleanup(func() { _ = client.Close() })
	require.NoError(t, client.Ping(context.Background()).Err())
	cache := NewConcurrencyCache(client, 15, 900)
	type result struct {
		err     error
		elapsed time.Duration
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	for _, operation := range []func(context.Context) error{
		func(ctx context.Context) error {
			_, err := cache.AcquireUserSlot(ctx, 42, 3, "new-request")
			return err
		},
		func(ctx context.Context) error { return cache.ReleaseUserSlot(ctx, 43, "finished-request") },
	} {
		go func() {
			<-start
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			started := time.Now()
			err := operation(ctx)
			results <- result{err: err, elapsed: time.Since(started)}
		}()
	}
	close(start)
	for i := 0; i < 2; i++ {
		outcome := <-results
		require.Error(t, outcome.err)
		require.Less(t, outcome.elapsed, 2500*time.Millisecond, "concurrent admission and release must retain their two-second operation budget")
	}
	commands := make([]string, 0, 2)
	for len(commands) < 2 {
		select {
		case command := <-server.commands:
			commands = append(commands, command)
		default:
			t.Fatal("both concurrency operations must reach the real Redis socket")
		}
	}
	require.ElementsMatch(t, []string{"EVALSHA", "ZREM"}, commands)
}
