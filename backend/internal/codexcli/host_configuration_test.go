//go:build unit

package codexcli

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Official 0.165.0 loads the three Unix system files outside CODEX_HOME.
// Even an empty file, symlink, directory, or inspection error is incompatible
// with the promise that only this task's managed account config is effective.
func TestHostConfigurationSystemFilesAndInspectionErrorsFailClosed(t *testing.T) {
	paths := []string{"/etc/codex/config.toml", "/etc/codex/managed_config.toml", "/etc/codex/requirements.toml"}
	for _, platform := range []string{"linux", "darwin"} {
		for _, scenario := range []struct {
			name, path, code string
			err              error
		}{
			{"no host configuration", "", "", os.ErrNotExist},
			{"system defaults", paths[0], "host_configuration_conflict", nil},
			{"legacy managed", paths[1], "host_configuration_conflict", nil},
			{"system requirements", paths[2], "host_configuration_conflict", nil},
			{"unreadable system source", paths[1], "host_configuration_check_failed", os.ErrPermission},
		} {
			t.Run(platform+"/"+scenario.name, func(t *testing.T) {
				var inspected []string
				commands := 0
				probe := hostConfigurationProbe{platform: platform,
					lstat: func(path string) (os.FileInfo, error) {
						inspected = append(inspected, path)
						if path == scenario.path {
							return nil, scenario.err
						}
						return nil, os.ErrNotExist
					},
					run: func(_ context.Context, command *exec.Cmd) error {
						commands++
						_, err := io.WriteString(command.Stdout, "clear\n")
						return err
					},
				}
				err := probe.check(t.Context())
				if scenario.code == "" {
					if err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(inspected, paths) {
						t.Fatalf("system sources = %v", inspected)
					}
					wantCommands := 0
					if platform == "darwin" {
						wantCommands = 1
					}
					if commands != wantCommands {
						t.Fatalf("managed preference probes = %d", commands)
					}
				} else {
					if err == nil || hostConfigurationErrorCode(err) != scenario.code {
						t.Fatalf("host check = %v; want %s", err, scenario.code)
					}
					if commands != 0 {
						t.Fatal("a conflicting file must stop before the MDM subprocess")
					}
				}
			})
		}
	}
}

// The command boundary accepts only the two documented boolean outcomes.
// Native preference values, stderr and OS failures never reach a client.
func TestHostConfigurationManagedPreferenceOutcomesAreStrict(t *testing.T) {
	for _, scenario := range []struct {
		name, output, code string
		err                error
	}{
		{"neither key forced", "clear\n", "", nil},
		{"config key forced", "managed\n", "host_configuration_conflict", nil},
		{"requirements key forced", "managed\n", "host_configuration_conflict", nil},
		{"missing command", "", "host_configuration_check_failed", exec.ErrNotFound},
		{"command failure", "clear\n", "host_configuration_check_failed", errors.New("private native preference diagnostics")},
		{"empty result", "", "host_configuration_check_failed", nil},
		{"missing newline", "clear", "host_configuration_check_failed", nil},
		{"extra prefix", "\nclear\n", "host_configuration_check_failed", nil},
		{"extra suffix", "clear\nmanaged\n", "host_configuration_check_failed", nil},
		{"whitespace", "clear \n", "host_configuration_check_failed", nil},
		{"unknown result", "false\n", "host_configuration_check_failed", nil},
		{"oversized result", strings.Repeat("x", 4097), "host_configuration_check_failed", nil},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			probe := hostConfigurationProbe{platform: "darwin",
				lstat: func(string) (os.FileInfo, error) { return nil, os.ErrNotExist },
				run: func(ctx context.Context, command *exec.Cmd) error {
					deadline, ok := ctx.Deadline()
					if !ok || time.Until(deadline) > 5*time.Second || time.Until(deadline) <= 0 {
						t.Fatal("MDM query must have a positive deadline capped at five seconds")
					}
					if command.Path != "/usr/bin/osascript" || !reflect.DeepEqual(command.Args[:4], []string{"/usr/bin/osascript", "-l", "JavaScript", "-e"}) || len(command.Args) != 5 {
						t.Fatalf("unsafe MDM command: %v", command.Args)
					}
					script := command.Args[4]
					if !strings.Contains(script, "'com.openai.codex'") ||
						!strings.Contains(script, "objectIsForcedForKeyInDomain('config_toml_base64', domain)") ||
						!strings.Contains(script, "objectIsForcedForKeyInDomain('requirements_toml_base64', domain)") ||
						strings.Count(script, "objectIsForcedForKeyInDomain(") != 2 || !strings.Contains(script, "||") {
						t.Fatal("both exact forced keys must independently reject managed configuration")
					}
					if strings.Contains(script, "objectForKey") || strings.Contains(script, "CopyAppValue") || strings.Contains(script, "persistentDomain") {
						t.Fatal("MDM guard must never read preference contents")
					}
					if !reflect.DeepEqual(command.Env, []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "LANG=C.UTF-8"}) || command.Dir != "/" || command.Stdin != nil || command.Stderr != io.Discard {
						t.Fatal("MDM query inherited process state or diagnostic output")
					}
					_, _ = io.WriteString(command.Stderr, "private stderr must not be exposed")
					_, err := io.WriteString(command.Stdout, scenario.output)
					if scenario.err != nil {
						return scenario.err
					}
					return err
				},
			}
			err := probe.check(t.Context())
			if scenario.code == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || hostConfigurationErrorCode(err) != scenario.code {
				t.Fatalf("MDM check = %v; want %s", err, scenario.code)
			}
			if err != nil && (strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "/usr/")) {
				t.Fatalf("host check exposed subprocess diagnostics: %v", err)
			}
		})
	}
}

func TestHostConfigurationManagedPreferenceCancellationAndDeadlineFailClosed(t *testing.T) {
	for _, cancelledBeforeCheck := range []bool{false, true} {
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
		if cancelledBeforeCheck {
			cancel()
		}
		commands := 0
		probe := hostConfigurationProbe{platform: "darwin",
			lstat: func(string) (os.FileInfo, error) { return nil, os.ErrNotExist },
			run:   func(ctx context.Context, _ *exec.Cmd) error { commands++; <-ctx.Done(); return ctx.Err() },
		}
		err := probe.check(ctx)
		cancel()
		if err == nil || hostConfigurationErrorCode(err) != "host_configuration_check_failed" {
			t.Fatalf("cancelled host check = %v", err)
		}
		if cancelledBeforeCheck && commands != 0 {
			t.Fatal("already cancelled check started a command")
		}
		if !cancelledBeforeCheck && commands != 1 {
			t.Fatal("deadline regression did not reach the preference command boundary")
		}
	}
}

func TestHostConfigurationStatusRetainsInstallationEvidenceWhileBlockingExecution(t *testing.T) {
	for _, code := range []string{"host_configuration_conflict", "host_configuration_check_failed"} {
		for _, sourceState := range []string{"verified", "version mismatch", "integrity failure", "missing"} {
			t.Run(code+"/"+sourceState, func(t *testing.T) {
				archive := validFixture(t)
				manager := fixtureManager(t, archive)
				if sourceState != "missing" {
					seedManaged(t, manager, archive, testExecutable)
				}
				if sourceState == "integrity failure" {
					seedManaged(t, manager, archive, "replacement executable")
				}
				if sourceState == "version mismatch" {
					manager.version = func(context.Context, string) (string, error) { return "0.164.0", nil }
				}
				before := manager.Status(t.Context())
				manager.hostCheck = func(context.Context) error { return &hostConfigurationError{code: code} }
				after := manager.Status(t.Context())
				if after.Path != before.Path || after.Source != before.Source || after.Installed != before.Installed || after.InstalledVersion != before.InstalledVersion || after.SourceVerified != before.SourceVerified || after.VersionMatch != before.VersionMatch {
					t.Fatalf("host conflict erased runtime evidence: before=%+v after=%+v", before, after)
				}
				if after.ExecutionAvailable || after.CanInstall || after.proof != nil || after.UnavailableCode != code {
					t.Fatalf("host configuration still authorizes execution or installation: %+v", after)
				}
			})
		}
	}
}

func TestHostConfigurationInstallStopsBeforeDownloadAndPublication(t *testing.T) {
	for _, code := range []string{"host_configuration_conflict", "host_configuration_check_failed"} {
		t.Run(code, func(t *testing.T) {
			manager := fixtureManager(t, validFixture(t))
			manager.hostCheck = func(context.Context) error { return &hostConfigurationError{code: code} }
			downloads := 0
			manager.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				downloads++
				return nil, errors.New("unexpected download")
			})
			status, err := manager.Install(t.Context())
			if err == nil || hostConfigurationErrorCode(err) != code || status.UnavailableCode != code || status.CanInstall || status.ExecutionAvailable || downloads != 0 {
				t.Fatalf("unsafe blocked installation: status=%+v error=%v downloads=%d", status, err, downloads)
			}
			if _, err := os.Stat(manager.root); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("blocked installer created files: %v", err)
			}
		})
	}
}

func TestHostConfigurationInstallRechecksAfterWaitingForInstallationLock(t *testing.T) {
	manager := fixtureManager(t, validFixture(t))
	checks, downloads := 0, 0
	manager.hostCheck = func(context.Context) error {
		checks++
		if checks >= 3 {
			return &hostConfigurationError{code: hostConfigurationConflict}
		}
		return nil
	}
	manager.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		downloads++
		return nil, errors.New("unexpected download")
	})
	status, err := manager.Install(t.Context())
	if err == nil || status.UnavailableCode != "host_configuration_conflict" || downloads != 0 || checks < 3 {
		t.Fatalf("host conflict after admission was ignored: %+v %v checks=%d downloads=%d", status, err, checks, downloads)
	}
	assertNoInstallTemps(t, manager)
}

// A previously verified status is insufficient: host policy may change between
// settings detection and the task request. The runner must reject before
// creating environment state or invoking the selected official executable.
func TestHostConfigurationTaskRechecksBeforeStartingPreviouslyReadyRuntime(t *testing.T) {
	for _, code := range []string{"host_configuration_conflict", "host_configuration_check_failed"} {
		t.Run(code, func(t *testing.T) {
			request := taskFixture(t, "printf started > \"$HOME/started\"\n"+emitTaskEvents(taskFinal, taskCompleted))
			checks := 0
			result, err := runTaskWithHostCheck(t.Context(), request, func(context.Context) error { checks++; return &hostConfigurationError{code: code} })
			assertTaskCode(t, err, code)
			if result.Started || checks != 1 {
				t.Fatalf("task started despite changed host policy: %+v checks=%d", result, checks)
			}
			for _, name := range []string{"started", ".tmp"} {
				if _, err := os.Stat(filepath.Join(request.Home, name)); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("blocked task created %s: %v", name, err)
				}
			}
		})
	}
}
