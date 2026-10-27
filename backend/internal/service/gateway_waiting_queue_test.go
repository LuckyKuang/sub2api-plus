//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDecrementWaitCount_NilCache 确保 nil cache 不会 panic
func TestDecrementWaitCount_NilCache(t *testing.T) {
	svc := &ConcurrencyService{cache: nil}
	// 不应 panic
	svc.DecrementWaitCount(context.Background(), 1)
}

// TestDecrementWaitCount_CacheError 确保 cache 错误不会传播
func TestDecrementWaitCount_CacheError(t *testing.T) {
	cache := &stubConcurrencyCacheForTest{}
	svc := NewConcurrencyService(cache)
	// DecrementWaitCount 使用 background context，错误只记录日志不传播
	svc.DecrementWaitCount(context.Background(), 1)
}

// TestDecrementAccountWaitCount_NilCache 确保 nil cache 不会 panic
func TestDecrementAccountWaitCount_NilCache(t *testing.T) {
	svc := &ConcurrencyService{cache: nil}
	svc.DecrementAccountWaitCount(context.Background(), 1)
}

// TestDecrementAccountWaitCount_CacheError 确保 cache 错误不会传播
func TestDecrementAccountWaitCount_CacheError(t *testing.T) {
	cache := &stubConcurrencyCacheForTest{}
	svc := NewConcurrencyService(cache)
	svc.DecrementAccountWaitCount(context.Background(), 1)
}

// TestWaitingQueueFlow_IncrementThenDecrement 测试完整的等待队列增减流程
func TestWaitingQueueFlow_IncrementThenDecrement(t *testing.T) {
	cache := &stubConcurrencyCacheForTest{waitAllowed: true}
	svc := NewConcurrencyService(cache)

	// 进入等待队列
	allowed, err := svc.IncrementWaitCount(context.Background(), 1, 25)
	require.NoError(t, err)
	require.True(t, allowed)

	// 离开等待队列（不应 panic）
	svc.DecrementWaitCount(context.Background(), 1)
}

// TestWaitingQueueFlow_AccountLevel 测试账号级等待队列流程
func TestWaitingQueueFlow_AccountLevel(t *testing.T) {
	cache := &stubConcurrencyCacheForTest{waitAllowed: true}
	svc := NewConcurrencyService(cache)

	// 进入账号等待队列
	allowed, err := svc.IncrementAccountWaitCount(context.Background(), 42, 10)
	require.NoError(t, err)
	require.True(t, allowed)

	// 离开账号等待队列
	svc.DecrementAccountWaitCount(context.Background(), 42)
}

// TestWaitingQueueFull_Returns429Signal 测试等待队列满时返回 false
func TestWaitingQueueFull_Returns429Signal(t *testing.T) {
	// waitAllowed=false 模拟队列已满
	cache := &stubConcurrencyCacheForTest{waitAllowed: false}
	svc := NewConcurrencyService(cache)

	// 用户级等待队列满
	allowed, err := svc.IncrementWaitCount(context.Background(), 1, 25)
	require.NoError(t, err)
	require.False(t, allowed, "等待队列满时应返回 false（调用方根据此返回 429）")

	// 账号级等待队列满
	allowed, err = svc.IncrementAccountWaitCount(context.Background(), 1, 10)
	require.NoError(t, err)
	require.False(t, allowed, "账号等待队列满时应返回 false")
}

// Redis failure cannot create an uncounted waiter outside the shared bound.
func TestWaitingQueue_FailClosed_OnCacheError(t *testing.T) {
	waitErr := errors.New("redis connection refused")
	cache := &stubConcurrencyCacheForTest{waitErr: waitErr}
	svc := NewConcurrencyService(cache)

	// A durable worker must prove ownership of its private user queue entry.
	allowed, err := svc.IncrementWaitCount(context.Background(), 1, 25)
	require.ErrorIs(t, err, waitErr)
	require.False(t, allowed, "an unconfirmed user waiter cannot enter the queue")

	// Shared account queues obey the same bound across users and instances.
	allowed, err = svc.IncrementAccountWaitCount(context.Background(), 1, 10)
	require.ErrorIs(t, err, waitErr)
	require.False(t, allowed, "an unconfirmed account waiter cannot enter the queue")
}

// TestCalculateMaxWait_Scenarios 测试最大等待队列大小计算
func TestCalculateMaxWait_Scenarios(t *testing.T) {
	tests := []struct {
		concurrency int
		expected    int
	}{
		{5, 25},    // 5 + 20
		{10, 30},   // 10 + 20
		{1, 21},    // 1 + 20
		{0, 21},    // min(1) + 20
		{-1, 21},   // min(1) + 20
		{-10, 21},  // min(1) + 20
		{100, 120}, // 100 + 20
	}
	for _, tt := range tests {
		result := CalculateMaxWait(tt.concurrency)
		require.Equal(t, tt.expected, result, "CalculateMaxWait(%d)", tt.concurrency)
	}
}
