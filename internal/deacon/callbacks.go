package deacon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/steveyegge/gastown/internal/mail"
	"github.com/steveyegge/gastown/internal/style"
)

// Callback type patterns — regexes for identifying message types
var (
	patternPolecatDone    = regexp.MustCompile(`^POLECAT_DONE\s+(\S+)`)
	patternMergeRejected  = regexp.MustCompile(`^Merge Request Rejected:\s+(.+)`)
	patternMergeCompleted = regexp.MustCompile(`^Merge Request Completed:\s+(.+)`)
	patternHelp           = regexp.MustCompile(`^HELP:\s+(.+)`)
	patternEscalation     = regexp.MustCompile(`^ESCALATION:\s+(.+)`)
	patternSling          = regexp.MustCompile(`^SLING_REQUEST:\s+(\S+)`)
)

// CallbackType represents the type of callback message
type CallbackType string

const (
	CallbackTypePolecat       CallbackType = "polecat_done"
	CallbackTypeMergeRejected CallbackType = "merge_rejected"
	CallbackTypeMergeComplete CallbackType = "merge_completed"
	CallbackTypeHelp          CallbackType = "help"
	CallbackTypeEscalation    CallbackType = "escalation"
	CallbackTypeSling         CallbackType = "sling"
	CallbackTypeUnknown       CallbackType = "unknown"
)

// CallbackState tracks processed callbacks to avoid duplicate handling.
// Maps message ID to timestamp when processed.
type CallbackState struct {
	// ProcessedMessages tracks which callbacks have been handled
	ProcessedMessages map[string]time.Time `json:"processed_messages"`

	// LastUpdated is when this state was last modified
	LastUpdated time.Time `json:"last_updated"`
}

// CallbackProcessResult represents the outcome of processing a single callback
type CallbackProcessResult struct {
	MessageID    string       `json:"message_id"`
	CallbackType CallbackType `json:"callback_type"`
	From         string       `json:"from"`
	Subject      string       `json:"subject"`
	Handled      bool         `json:"handled"`
	Action       string       `json:"action"`
	Error        string       `json:"error,omitempty"`
}

// CallbacksResult is the overall result from processing all callbacks
type CallbacksResult struct {
	Processed      int                      `json:"processed"`
	Failed         int                      `json:"failed"`
	Skipped        int                      `json:"skipped"`
	ProcessResults []CallbackProcessResult  `json:"process_results"`
	Message        string                   `json:"message"`
}

// callbackStateFile returns the path to the callbacks state file
func callbackStateFile(townRoot string) string {
	return filepath.Join(townRoot, "deacon", "callbacks-state.json")
}

// LoadCallbackState loads the callbacks state from disk.
// Returns an empty state if the file doesn't exist.
func LoadCallbackState(townRoot string) *CallbackState {
	stateFile := callbackStateFile(townRoot)

	data, err := os.ReadFile(stateFile) //nolint:gosec // G304: path constructed from trusted townRoot
	if err != nil {
		// File doesn't exist yet — start fresh
		return &CallbackState{
			ProcessedMessages: make(map[string]time.Time),
			LastUpdated:       time.Now().UTC(),
		}
	}

	var state CallbackState
	if err := json.Unmarshal(data, &state); err != nil {
		// Corrupted state file — start fresh
		return &CallbackState{
			ProcessedMessages: make(map[string]time.Time),
			LastUpdated:       time.Now().UTC(),
		}
	}

	// Ensure map is initialized
	if state.ProcessedMessages == nil {
		state.ProcessedMessages = make(map[string]time.Time)
	}

	return &state
}

// SaveCallbackState persists the callbacks state to disk
func SaveCallbackState(townRoot string, state *CallbackState) error {
	stateFile := callbackStateFile(townRoot)

	// Ensure deacon directory exists
	if err := os.MkdirAll(filepath.Dir(stateFile), 0755); err != nil {
		return fmt.Errorf("creating deacon directory: %w", err)
	}

	// Update timestamp
	state.LastUpdated = time.Now().UTC()

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling callbacks state: %w", err)
	}

	if err := os.WriteFile(stateFile, data, 0600); err != nil {
		return fmt.Errorf("writing callbacks state: %w", err)
	}

	return nil
}

// classifyCallback determines the callback type from the message subject
func classifyCallback(subject string) CallbackType {
	if patternPolecatDone.MatchString(subject) {
		return CallbackTypePolecat
	}
	if patternMergeRejected.MatchString(subject) {
		return CallbackTypeMergeRejected
	}
	if patternMergeCompleted.MatchString(subject) {
		return CallbackTypeMergeComplete
	}
	if patternHelp.MatchString(subject) {
		return CallbackTypeHelp
	}
	if patternEscalation.MatchString(subject) {
		return CallbackTypeEscalation
	}
	if patternSling.MatchString(subject) {
		return CallbackTypeSling
	}
	return CallbackTypeUnknown
}

// handlePolecatDone processes a polecat completion signal
// Extracts polecat name and logs completion status
func handlePolecatDone(msg *mail.Message) (string, error) {
	matches := patternPolecatDone.FindStringSubmatch(msg.Subject)
	if len(matches) < 2 {
		return "", fmt.Errorf("invalid POLECAT_DONE format")
	}

	polecatName := matches[1]
	bodyLines := strings.Split(msg.Body, "\n")

	var exitType string
	var issueID string
	for _, line := range bodyLines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "exit_type:") {
			exitType = strings.TrimSpace(strings.TrimPrefix(line, "exit_type:"))
		}
		if strings.HasPrefix(line, "issue_id:") {
			issueID = strings.TrimSpace(strings.TrimPrefix(line, "issue_id:"))
		}
	}

	action := fmt.Sprintf("Logged polecat %s completion (exit_type: %s, issue: %s)", polecatName, exitType, issueID)
	return action, nil
}

// handleMergeCompleted processes a successful merge notification
// Extracts branch and merge details from the callback
func handleMergeCompleted(msg *mail.Message) (string, error) {
	matches := patternMergeCompleted.FindStringSubmatch(msg.Subject)
	if len(matches) < 2 {
		return "", fmt.Errorf("invalid Merge Request Completed format")
	}

	branch := matches[1]
	bodyLines := strings.Split(msg.Body, "\n")

	var mrID string
	var sourceIssue string
	var mergeCommit string
	for _, line := range bodyLines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "mr_id:") {
			mrID = strings.TrimSpace(strings.TrimPrefix(line, "mr_id:"))
		}
		if strings.HasPrefix(line, "source_issue:") {
			sourceIssue = strings.TrimSpace(strings.TrimPrefix(line, "source_issue:"))
		}
		if strings.HasPrefix(line, "merge_commit:") {
			mergeCommit = strings.TrimSpace(strings.TrimPrefix(line, "merge_commit:"))
		}
	}

	action := fmt.Sprintf("Logged merge completion for %s (MR: %s, source: %s, commit: %s)", branch, mrID, sourceIssue, mergeCommit)
	return action, nil
}

// handleMergeRejected processes a merge rejection notification
// Extracts branch and rejection reason
func handleMergeRejected(msg *mail.Message) (string, error) {
	matches := patternMergeRejected.FindStringSubmatch(msg.Subject)
	if len(matches) < 2 {
		return "", fmt.Errorf("invalid Merge Request Rejected format")
	}

	branch := matches[1]
	bodyLines := strings.Split(msg.Body, "\n")

	var reason string
	for _, line := range bodyLines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "reason:") {
			reason = strings.TrimSpace(strings.TrimPrefix(line, "reason:"))
			break
		}
	}

	action := fmt.Sprintf("Logged merge rejection for %s (reason: %s)", branch, reason)
	return action, nil
}

// handleHelp processes a help request from an agent
// Extracts the help topic and logs it for review
func handleHelp(msg *mail.Message) (string, error) {
	matches := patternHelp.FindStringSubmatch(msg.Subject)
	if len(matches) < 2 {
		return "", fmt.Errorf("invalid HELP format")
	}

	topic := matches[1]
	action := fmt.Sprintf("Logged help request from %s on topic: %s", msg.From, topic)
	return action, nil
}

// handleEscalation processes an escalation from a witness
// Escalations require immediate attention from the overseer
func handleEscalation(msg *mail.Message) (string, error) {
	matches := patternEscalation.FindStringSubmatch(msg.Subject)
	if len(matches) < 2 {
		return "", fmt.Errorf("invalid ESCALATION format")
	}

	topic := matches[1]
	action := fmt.Sprintf("Logged escalation from %s on topic: %s - requires investigation", msg.From, topic)
	return action, nil
}

// handleSling processes a request to spawn work via the sling system
// Extracts the bead ID and target rig
func handleSling(msg *mail.Message) (string, error) {
	matches := patternSling.FindStringSubmatch(msg.Subject)
	if len(matches) < 2 {
		return "", fmt.Errorf("invalid SLING_REQUEST format")
	}

	beadID := matches[1]
	bodyLines := strings.Split(msg.Body, "\n")

	var targetRig string
	for _, line := range bodyLines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "target_rig:") {
			targetRig = strings.TrimSpace(strings.TrimPrefix(line, "target_rig:"))
			break
		}
	}

	action := fmt.Sprintf("Logged sling request for bead %s targeting rig %s", beadID, targetRig)
	return action, nil
}

// processCallback handles a single callback message
// Classifies the message, routes to handler, and returns result
func processCallback(msg *mail.Message, mailbox *mail.Mailbox) CallbackProcessResult {
	result := CallbackProcessResult{
		MessageID: msg.ID,
		From:      msg.From,
		Subject:   msg.Subject,
	}

	// Classify the callback
	callbackType := classifyCallback(msg.Subject)
	result.CallbackType = callbackType

	// Route to appropriate handler
	var action string
	var err error

	switch callbackType {
	case CallbackTypePolecat:
		action, err = handlePolecatDone(msg)
	case CallbackTypeMergeComplete:
		action, err = handleMergeCompleted(msg)
	case CallbackTypeMergeRejected:
		action, err = handleMergeRejected(msg)
	case CallbackTypeHelp:
		action, err = handleHelp(msg)
	case CallbackTypeEscalation:
		action, err = handleEscalation(msg)
	case CallbackTypeSling:
		action, err = handleSling(msg)
	default:
		result.Handled = false
		result.Error = "unknown callback type"
		return result
	}

	if err != nil {
		result.Handled = false
		result.Error = err.Error()
		return result
	}

	// Archive the message (delete from mailbox)
	if err := mailbox.Delete(msg.ID); err != nil {
		// Log warning but don't fail — message was processed
		style.PrintWarning("failed to archive callback %s: %v", msg.ID, err)
	}

	result.Handled = true
	result.Action = action
	return result
}

// ProcessCallbacks is the main patrol step function for handling callbacks.
// It loads the mayor's mailbox, processes all pending callbacks,
// and persists the state.
// This function is called from the deacon patrol loop.
func ProcessCallbacks(townRoot string) (*CallbacksResult, error) {
	result := &CallbacksResult{
		ProcessResults: []CallbackProcessResult{},
	}

	// Load current state to avoid re-processing
	state := LoadCallbackState(townRoot)

	// Create mail router to access mayor's mailbox
	router := mail.NewRouter(townRoot)
	mailbox, err := router.MayorMailbox()
	if err != nil {
		return result, fmt.Errorf("accessing mayor mailbox: %w", err)
	}

	// List unread messages from the mailbox
	messages, err := mailbox.ListUnread()
	if err != nil {
		return result, fmt.Errorf("listing unread messages: %w", err)
	}

	// Process each message
	for _, msg := range messages {
		// Skip if already processed
		if _, processed := state.ProcessedMessages[msg.ID]; processed {
			result.Skipped++
			continue
		}

		// Process the callback
		procResult := processCallback(&msg, mailbox)
		result.ProcessResults = append(result.ProcessResults, procResult)

		if procResult.Handled {
			result.Processed++
			state.ProcessedMessages[msg.ID] = time.Now().UTC()
		} else {
			result.Failed++
		}
	}

	// Persist updated state
	if err := SaveCallbackState(townRoot, state); err != nil {
		// Log warning but don't fail — callbacks were still processed
		style.PrintWarning("failed to save callbacks state: %v", err)
	}

	// Generate summary message
	result.Message = fmt.Sprintf("Processed %d callbacks (%d successful, %d failed, %d skipped)",
		len(messages), result.Processed, result.Failed, result.Skipped)

	return result, nil
}
