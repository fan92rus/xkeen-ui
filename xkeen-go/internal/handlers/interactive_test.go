package handlers

import (
	"encoding/json"
	"testing"
)

func TestNewInteractiveHandler(t *testing.T) {
	handler := NewInteractiveHandler(nil)
	if handler == nil {
		t.Fatal("Expected non-nil handler")
	}
	if handler.allowedCommands == nil {
		t.Error("Expected allowedCommands to be initialized")
	}
	if len(handler.allowedCommands) == 0 {
		t.Error("Expected non-empty allowedCommands")
	}
}

func TestInteractiveMessageTypes(t *testing.T) {
	// Test ClientMessage parsing
	clientJSON := `{"type":"start","command":"uk"}`
	var clientMsg ClientMessage
	if err := json.Unmarshal([]byte(clientJSON), &clientMsg); err != nil {
		t.Fatalf("Failed to parse ClientMessage: %v", err)
	}
	if clientMsg.Type != "start" || clientMsg.Command != "uk" {
		t.Errorf("Unexpected ClientMessage values: %+v", clientMsg)
	}

	// Test input message
	inputJSON := `{"type":"input","text":"2.3.1\n"}`
	var inputMsg ClientMessage
	if err := json.Unmarshal([]byte(inputJSON), &inputMsg); err != nil {
		t.Fatalf("Failed to parse input message: %v", err)
	}
	if inputMsg.Type != "input" || inputMsg.Text != "2.3.1\n" {
		t.Errorf("Unexpected input message values: %+v", inputMsg)
	}

	// Test ServerMessage
	serverMsg := ServerMessage{Type: "output", Text: "hello"}
	data, err := json.Marshal(serverMsg)
	if err != nil {
		t.Fatalf("Failed to marshal ServerMessage: %v", err)
	}
	if string(data) != `{"type":"output","text":"hello"}` {
		t.Errorf("Unexpected ServerMessage JSON: %s", data)
	}
}
