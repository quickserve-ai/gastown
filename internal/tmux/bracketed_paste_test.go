package tmux

import (
	"strings"
	"testing"
	"time"
)

// TestBracketedPaste_DeliversContent verifies that bracketedPaste lands the
// payload in the target pane's input. The leading characters of the marker are
// chosen to be pure vim NORMAL-mode motions (h/j/k/l) so that, against a
// vim-mode receiver, a raw send-keys would have eaten them — bracketed paste
// must deliver them verbatim. (hq-5ktear)
func TestBracketedPaste_DeliversContent(t *testing.T) {
	tm := newTestTmux(t)
	sessionName := "gt-test-bpaste-" + t.Name()

	_ = tm.KillSession(sessionName)
	if err := tm.NewSession(sessionName, ""); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer func() { _ = tm.KillSession(sessionName) }()

	// Give the shell a moment to present its prompt.
	time.Sleep(300 * time.Millisecond)

	const marker = "hjklBRACKETED_PASTE_MARKER_12345"
	if err := tm.bracketedPaste(sessionName, marker); err != nil {
		t.Fatalf("bracketedPaste: %v", err)
	}

	// Poll the pane for the marker (no Enter is sent, so it stays on the input
	// line). Delivery must land it whether or not the receiver supports
	// bracketed paste: if supported it is inserted literally; if not, tmux
	// sends the raw bytes, which still appear on the command line.
	deadline := time.Now().Add(3 * time.Second)
	var output string
	for time.Now().Before(deadline) {
		out, err := tm.CapturePane(sessionName, 50)
		if err != nil {
			t.Fatalf("CapturePane: %v", err)
		}
		output = out
		if strings.Contains(output, marker) {
			return // delivered
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Errorf("marker %q not found in pane after delivery; captured:\n%s", marker, output)
}

// TestBracketedPaste_DeletesBuffer verifies that bracketedPaste does not leak
// tmux paste buffers — paste-buffer -d removes the buffer after pasting.
func TestBracketedPaste_DeletesBuffer(t *testing.T) {
	tm := newTestTmux(t)
	sessionName := "gt-test-bpaste-buf-" + t.Name()

	_ = tm.KillSession(sessionName)
	if err := tm.NewSession(sessionName, ""); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer func() { _ = tm.KillSession(sessionName) }()

	time.Sleep(200 * time.Millisecond)

	if err := tm.bracketedPaste(sessionName, "leak-check-payload"); err != nil {
		t.Fatalf("bracketedPaste: %v", err)
	}

	// No gt-nudge-* buffer should remain after a successful paste.
	buffers, err := tm.run("list-buffers")
	if err != nil {
		// No buffers at all -> list-buffers may return empty/no error; tolerate.
		return
	}
	if strings.Contains(buffers, "gt-nudge-") {
		t.Errorf("paste buffer leaked; list-buffers:\n%s", buffers)
	}
}

// TestSendMessageToTarget_DeliversContent verifies the public delivery entry
// point (used by NudgeSession/NudgePane) lands content in the target pane.
func TestSendMessageToTarget_DeliversContent(t *testing.T) {
	tm := newTestTmux(t)
	sessionName := "gt-test-sendmsg-" + t.Name()

	_ = tm.KillSession(sessionName)
	if err := tm.NewSession(sessionName, ""); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer func() { _ = tm.KillSession(sessionName) }()

	time.Sleep(300 * time.Millisecond)

	const marker = "kkjjSENDMSG_MARKER_67890"
	if err := tm.sendMessageToTarget(sessionName, marker); err != nil {
		t.Fatalf("sendMessageToTarget: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	var output string
	for time.Now().Before(deadline) {
		out, err := tm.CapturePane(sessionName, 50)
		if err != nil {
			t.Fatalf("CapturePane: %v", err)
		}
		output = out
		if strings.Contains(output, marker) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Errorf("marker %q not found in pane after delivery; captured:\n%s", marker, output)
}

// TestSendKeysChunked_LargeMessage verifies the legacy fallback path still
// delivers messages larger than the chunk size.
func TestSendKeysChunked_LargeMessage(t *testing.T) {
	tm := newTestTmux(t)
	sessionName := "gt-test-chunked-" + t.Name()

	_ = tm.KillSession(sessionName)
	if err := tm.NewSession(sessionName, ""); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer func() { _ = tm.KillSession(sessionName) }()

	time.Sleep(200 * time.Millisecond)

	// A payload larger than sendKeysChunkSize, with a recognizable tail so we
	// can confirm the final chunk arrived.
	payload := strings.Repeat("a", sendKeysChunkSize) + "CHUNK_TAIL_MARKER"
	if err := tm.sendKeysChunked(sessionName, payload); err != nil {
		t.Fatalf("sendKeysChunked: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	var output string
	for time.Now().Before(deadline) {
		out, err := tm.CapturePane(sessionName, 50)
		if err != nil {
			t.Fatalf("CapturePane: %v", err)
		}
		output = out
		if strings.Contains(output, "CHUNK_TAIL_MARKER") {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Errorf("chunk tail marker not found after delivery; captured:\n%s", output)
}
