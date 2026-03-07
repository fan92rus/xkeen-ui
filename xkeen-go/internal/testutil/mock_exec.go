package testutil

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// CommandRecord stores information about an executed command for testing verification.
type CommandRecord struct {
	Name      string
	Args      []string
	Output    []byte
	Error     error
	Timestamp time.Time
	Env       map[string]string
	Dir       string
}

// MockExecutor simulates command execution for testing purposes.
// It captures command calls and allows configuring predefined responses.
type MockExecutor struct {
	mu       sync.RWMutex
	commands []CommandRecord
	results  map[string]commandResult
	// Default behavior configuration
	defaultOutput []byte
	defaultError  error
}

type commandResult struct {
	output []byte
	err    error
}

// NewMockExecutor creates a new mock command executor with default settings.
func NewMockExecutor() *MockExecutor {
	return &MockExecutor{
		commands: make([]CommandRecord, 0),
		results:  make(map[string]commandResult),
		defaultOutput: []byte("OK\n"),
		defaultError:  nil,
	}
}

// SetResult configures what output and error to return for a specific command name.
// This allows simulating different command behaviors.
func (m *MockExecutor) SetResult(name string, output []byte, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.results[name] = commandResult{
		output: output,
		err:    err,
	}
}

// SetResultWithArgs configures result for a command with specific arguments.
// This allows more granular control over command responses.
func (m *MockExecutor) SetResultWithArgs(name string, args []string, output []byte, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := m.buildKey(name, args)
	m.results[key] = commandResult{
		output: output,
		err:    err,
	}
}

// SetDefaultOutput sets the default output for commands without configured results.
func (m *MockExecutor) SetDefaultOutput(output []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.defaultOutput = output
}

// SetDefaultError sets the default error for commands without configured results.
func (m *MockExecutor) SetDefaultError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.defaultError = err
}

// Execute simulates command execution and records the call.
// Returns configured result or default values if not configured.
func (m *MockExecutor) Execute(name string, args ...string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	record := CommandRecord{
		Name:      name,
		Args:      args,
		Timestamp: time.Now(),
		Dir:       "",
		Env:       make(map[string]string),
	}

	// Try to find result with exact args first
	key := m.buildKey(name, args)
	if result, ok := m.results[key]; ok {
		record.Output = result.output
		record.Error = result.err
		m.commands = append(m.commands, record)
		return result.output, result.err
	}

	// Try to find result by command name only
	if result, ok := m.results[name]; ok {
		record.Output = result.output
		record.Error = result.err
		m.commands = append(m.commands, record)
		return result.output, result.err
	}

	// Use default values
	record.Output = m.defaultOutput
	record.Error = m.defaultError
	m.commands = append(m.commands, record)
	return m.defaultOutput, m.defaultError
}

// ExecuteWithEnv executes a command with environment variables.
func (m *MockExecutor) ExecuteWithEnv(name string, args []string, env map[string]string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	record := CommandRecord{
		Name:      name,
		Args:      args,
		Timestamp: time.Now(),
		Env:       env,
		Dir:       "",
	}

	// Try to find result
	key := m.buildKey(name, args)
	if result, ok := m.results[key]; ok {
		record.Output = result.output
		record.Error = result.err
		m.commands = append(m.commands, record)
		return result.output, result.err
	}

	if result, ok := m.results[name]; ok {
		record.Output = result.output
		record.Error = result.err
		m.commands = append(m.commands, record)
		return result.output, result.err
	}

	record.Output = m.defaultOutput
	record.Error = m.defaultError
	m.commands = append(m.commands, record)
	return m.defaultOutput, m.defaultError
}

// ExecuteInDir executes a command in a specific directory.
func (m *MockExecutor) ExecuteInDir(dir, name string, args ...string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	record := CommandRecord{
		Name:      name,
		Args:      args,
		Timestamp: time.Now(),
		Dir:       dir,
		Env:       make(map[string]string),
	}

	// Try to find result
	key := m.buildKey(name, args)
	if result, ok := m.results[key]; ok {
		record.Output = result.output
		record.Error = result.err
		m.commands = append(m.commands, record)
		return result.output, result.err
	}

	if result, ok := m.results[name]; ok {
		record.Output = result.output
		record.Error = result.err
		m.commands = append(m.commands, record)
		return result.output, result.err
	}

	record.Output = m.defaultOutput
	record.Error = m.defaultError
	m.commands = append(m.commands, record)
	return m.defaultOutput, m.defaultError
}

// GetCommands returns all recorded command calls.
// Returns a copy to prevent modification of internal state.
func (m *MockExecutor) GetCommands() []CommandRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()

	commands := make([]CommandRecord, len(m.commands))
	copy(commands, m.commands)
	return commands
}

// GetCommandsByName returns all command calls for a specific command name.
func (m *MockExecutor) GetCommandsByName(name string) []CommandRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []CommandRecord
	for _, cmd := range m.commands {
		if cmd.Name == name {
			result = append(result, cmd)
		}
	}
	return result
}

// GetLastCommand returns the most recent command call.
// Returns an error if no commands have been executed.
func (m *MockExecutor) GetLastCommand() (CommandRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.commands) == 0 {
		return CommandRecord{}, fmt.Errorf("no commands have been executed")
	}
	return m.commands[len(m.commands)-1], nil
}

// GetCommandCount returns the number of times a specific command was executed.
func (m *MockExecutor) GetCommandCount(name string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count := 0
	for _, cmd := range m.commands {
		if cmd.Name == name {
			count++
		}
	}
	return count
}

// TotalCommandCount returns the total number of commands executed.
func (m *MockExecutor) TotalCommandCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.commands)
}

// WasCommandCalled checks if a specific command was called with the given arguments.
func (m *MockExecutor) WasCommandCalled(name string, args ...string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, cmd := range m.commands {
		if cmd.Name == name && m.argsEqual(cmd.Args, args) {
			return true
		}
	}
	return false
}

// WasCommandCalledContaining checks if a command was called with args containing the specified values.
func (m *MockExecutor) WasCommandCalledContaining(name string, args ...string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, cmd := range m.commands {
		if cmd.Name != name {
			continue
		}
		if m.argsContain(cmd.Args, args) {
			return true
		}
	}
	return false
}

// Reset clears all recorded commands but keeps configured results.
func (m *MockExecutor) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.commands = make([]CommandRecord, 0)
}

// ResetAll clears both recorded commands and configured results.
func (m *MockExecutor) ResetAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.commands = make([]CommandRecord, 0)
	m.results = make(map[string]commandResult)
	m.defaultOutput = []byte("OK\n")
	m.defaultError = nil
}

// ClearResults clears only configured results, keeping command history.
func (m *MockExecutor) ClearResults() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.results = make(map[string]commandResult)
}

// SeedWithXkeenCommands configures typical xkeen command responses.
func (m *MockExecutor) SeedWithXkeenCommands() {
	// xkeen start command
	m.SetResult("xkeen", []byte("Xray started successfully\n"), nil)

	// pidof command (process running)
	m.SetResult("pidof", []byte("12345\n"), nil)

	// kill command
	m.SetResult("kill", []byte(""), nil)

	// Standard success output
	m.SetResult("echo", []byte("OK\n"), nil)

	// Configure command with args for specific operations
	m.SetResultWithArgs("xkeen", []string{"-start"}, []byte("Xray started successfully\n"), nil)
	m.SetResultWithArgs("xkeen", []string{"-stop"}, []byte("Xray stopped successfully\n"), nil)
	m.SetResultWithArgs("xkeen", []string{"-restart"}, []byte("Xray restarted successfully\n"), nil)
	m.SetResultWithArgs("xkeen", []string{"-status"}, []byte("Xray is running (PID: 12345)\n"), nil)
	m.SetResultWithArgs("xkeen", []string{"-check"}, []byte("Config OK\n"), nil)
}

// SetXkeenRunning configures responses as if xkeen/xray is running.
func (m *MockExecutor) SetXkeenRunning(running bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if running {
		m.results["pidof"] = commandResult{
			output: []byte("12345\n"),
			err:    nil,
		}
		m.SetResultWithArgs("xkeen", []string{"-status"}, []byte("Xray is running (PID: 12345)\n"), nil)
	} else {
		m.results["pidof"] = commandResult{
			output: []byte(""),
			err:    fmt.Errorf("process not found"),
		}
		m.SetResultWithArgs("xkeen", []string{"-status"}, []byte("Xray is not running\n"), nil)
	}
}

// SetConfigCheckResult configures the response for config validation.
func (m *MockExecutor) SetConfigCheckResult(valid bool, message string) {
	if valid {
		m.SetResultWithArgs("xkeen", []string{"-check"}, []byte(message+"\n"), nil)
	} else {
		m.SetResultWithArgs("xkeen", []string{"-check"}, []byte(message+"\n"), fmt.Errorf("config validation failed"))
	}
}

// buildKey creates a unique key for command + args combination.
func (m *MockExecutor) buildKey(name string, args []string) string {
	return name + "|" + strings.Join(args, "|")
}

// argsEqual checks if two argument slices are equal.
func (m *MockExecutor) argsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// argsContain checks if args contains all of the specified values in order.
func (m *MockExecutor) argsContain(args, contains []string) bool {
	if len(contains) == 0 {
		return true
	}
	if len(args) < len(contains) {
		return false
	}

	for i := 0; i <= len(args)-len(contains); i++ {
		match := true
		for j, c := range contains {
			if args[i+j] != c {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// CommandExecutor is an interface that both MockExecutor and real executors should implement.
type CommandExecutor interface {
	Execute(name string, args ...string) ([]byte, error)
}

// Ensure MockExecutor implements CommandExecutor
var _ CommandExecutor = (*MockExecutor)(nil)
