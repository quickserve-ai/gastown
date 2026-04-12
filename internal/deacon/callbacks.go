package deacon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/mail"
	"github.com/steveyegge/gastown/internal/style"
	"github.com/steveyegge/gastown/internal/townlog"
	"github.com/steveyegge/gastown/internal/witness"
)

// Callback type patterns — regexes for identifying message types from subject lines.
var (
	PatternPolecatDone    = regexp.MustCompile(`^POLECAT_DONE\s+(\S+)`)
	PatternMergeRejected  = regexp.MustCompile(`^Merge Request Rejected:\s+(.+)`)
	PatternMergeCompleted = regexp.MustCompile(`^Merge Request Completed:\s+(.+)`)
	PatternHelp           = regexp.MustCompile(`^HELP:\s+(.+)`)
	PatternEscalation     = regexp.MustCompile(`^ESCALATION:\s+(.+)`)
	PatternSling          = regexp.MustCompile(`^SLING_REQUEST:\s+(\S+)`)
)

// CallbackType represents the type of callback message.
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
type CallbackState struct {
	ProcessedMessages map[string]time.Time `json:"processed_messages"`
	LastUpdated       time.Time            `json:"last_updated"`
}

// CallbackProcessResult represents the outcome of processing a single callback.
type CallbackProcessResult struct {
	MessageID    string       `json:"message_id"`
	CallbackType CallbackType `json:"callback_type"`
	From         string       `json:"from"`
	Subject      string       `json:"subject"`
	Handled      bool         `json:"handled"`
	Action       string       `json:"action"`
	Error        string       `json:"error,omitempty"`
}

// CallbacksResult is the overall result from processing all callbacks.
type CallbacksResult struct {
	Processed      int                     `json:"processed"`
	Failed         int                     `json:"failed"`
	Skipped        int                     `json:"skipped"`
	ProcessResults []CallbackProcessResult `json:"process_results"`
	Message        string                  `json:"message"`
}

// callbackStateFile returns the path to the callbacks state file.
func callbackStateFile(townRoot string) string {
	return filepath.Join(townRoot, "deacon", "callbacks-state.json")
}

// LoadCallbackState loads the callbacks state from disk.
func LoadCallbackState(townRoot string) *CallbackState {
	stateFile := callbackStateFile(townRoot)

	data, err := os.ReadFile(stateFile) //nolint:gosec // G304: path constructed from trusted townRoot
	if err != nil {
		return &CallbackState{
			ProcessedMessages: make(map[string]time.Time),
			LastUpdated:       time.Now().UTC(),
		}
	}

	var state CallbackState
	if err := json.Unmarshal(data, &state); err != nil {
		return &CallbackState{
			ProcessedMessages: make(map[string]time.Time),
			LastUpdated:       time.Now().UTC(),
		}
	}

	if state.ProcessedMessages == nil {
		state.ProcessedMessages = make(map[string]time.Time)
	}

	return &state
}

// SaveCallbackState persists the callbacks state to disk.
func SaveCallbackState(townRoot string, state *CallbackState) error {
	stateFile := callbackStateFile(townRoot)

	if err := os.MkdirAll(filepath.Dir(stateFile), 0755); err != nil {
		return fmt.Errorf("creating deacon directory: %w", err)
	}

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

// ClassifyCallback determines the callback type from the message subject.
func ClassifyCallback(subject string) CallbackType {
	switch {
	case PatternPolecatDone.MatchString(subject):
		return CallbackTypePolecat
	case PatternMergeRejected.MatchString(subject):
		return CallbackTypeMergeRejected
	case PatternMergeCompleted.MatchString(subject):
		return CallbackTypeMergeComplete
	case PatternHelp.MatchString(subject):
		return CallbackTypeHelp
	case PatternEscalation.MatchString(subject):
		return CallbackTypeEscalation
	case PatternSling.MatchString(subject):
		return CallbackTypeSling
	default:
		return CallbackTypeUnknown
	}
}

// HandlePolecatDone processes a POLECAT_DONE callback.
func HandlePolecatDone(townRoot string, msg *mail.Message, dryRun bool) (string, error) {
	matches := PatternPolecatDone.FindStringSubmatch(msg.Subject)
	if len(matches) < 2 {
		return "", fmt.Errorf("could not parse polecat name from subject: %q", msg.Subject)
	}
	polecatName := matches[1]

	var exitType, issueID string
	for _, line := range strings.Split(msg.Body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Exit:") {
			exitType = strings.TrimSpace(strings.TrimPrefix(line, "Exit:"))
		}
		if strings.HasPrefix(line, "Issue:") {
			issueID = strings.TrimSpace(strings.TrimPrefix(line, "Issue:"))
		}
	}

	if dryRun {
		return fmt.Sprintf("would log completion for %s (exit=%s, issue=%s)",
			polecatName, exitType, issueID), nil
	}

	logCallback(townRoot, fmt.Sprintf("polecat_done: %s completed with %s (issue: %s)",
		msg.From, exitType, issueID))

	return fmt.Sprintf("logged completion for %s", polecatName), nil
}

// HandleMergeCompleted processes a merge completion callback from Refinery.
func HandleMergeCompleted(townRoot string, msg *mail.Message, dryRun bool) (string, error) {
	matches := PatternMergeCompleted.FindStringSubmatch(msg.Subject)
	if len(matches) < 2 {
		return "", fmt.Errorf("could not parse branch from subject: %q", msg.Subject)
	}
	branch := matches[1]

	var mrID, sourceIssue, mergeCommit string
	for _, line := range strings.Split(msg.Body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "MR:") {
			mrID = strings.TrimSpace(strings.TrimPrefix(line, "MR:"))
		}
		if strings.HasPrefix(line, "Source:") {
			sourceIssue = strings.TrimSpace(strings.TrimPrefix(line, "Source:"))
		}
		if strings.HasPrefix(line, "Commit:") {
			mergeCommit = strings.TrimSpace(strings.TrimPrefix(line, "Commit:"))
		}
	}

	if dryRun {
		return fmt.Sprintf("would close source issue %s (mr=%s, commit=%s)",
			sourceIssue, mrID, mergeCommit), nil
	}

	logCallback(townRoot, fmt.Sprintf("merge_completed: branch %s merged (mr=%s, source=%s, commit=%s)",
		branch, mrID, sourceIssue, mergeCommit))

	if sourceIssue != "" {
		bd := beads.New(townRoot)
		reason := fmt.Sprintf("Merged in %s", mergeCommit)
		if err := bd.CloseWithReason(reason, sourceIssue); err != nil {
			return fmt.Sprintf("logged merge for %s (could not close %s: %v)",
				branch, sourceIssue, err), nil
		}
	}

	return fmt.Sprintf("logged merge for %s, closed %s", branch, sourceIssue), nil
}

// HandleMergeRejected processes a merge rejection callback from Refinery.
func HandleMergeRejected(townRoot string, msg *mail.Message, dryRun bool) (string, error) {
	matches := PatternMergeRejected.FindStringSubmatch(msg.Subject)
	if len(matches) < 2 {
		return "", fmt.Errorf("could not parse branch from subject: %q", msg.Subject)
	}
	branch := matches[1]

	var reason string
	if strings.Contains(msg.Body, "Reason:") {
		parts := strings.SplitN(msg.Body, "Reason:", 2)
		if len(parts) > 1 {
			reason = strings.TrimSpace(parts[1])
			if idx := strings.Index(reason, "\n"); idx > 0 {
				reason = reason[:idx]
			}
		}
	}

	if dryRun {
		return fmt.Sprintf("would log rejection for %s (reason: %s)", branch, reason), nil
	}

	logCallback(townRoot, fmt.Sprintf("merge_rejected: branch %s rejected: %s", branch, reason))

	return fmt.Sprintf("logged rejection for %s", branch), nil
}

// HandleHelp processes a HELP: request from a polecat.
// Assesses category and severity and forwards to the overseer.
func HandleHelp(townRoot string, msg *mail.Message, dryRun bool) (string, error) {
	payload, err := witness.ParseHelp(msg.Subject, msg.Body)
	if err != nil {
		return "", fmt.Errorf("could not parse help request: %w", err)
	}

	assessment := witness.AssessHelp(payload)

	if dryRun {
		return fmt.Sprintf("would forward help request to overseer: %s [%s/%s]",
			payload.Topic, assessment.Category, assessment.Severity), nil
	}

	var priority mail.Priority
	switch assessment.Severity {
	case witness.HelpSeverityCritical:
		priority = mail.PriorityUrgent
	case witness.HelpSeverityHigh:
		priority = mail.PriorityHigh
	default:
		priority = mail.PriorityNormal
	}

	router := mail.NewRouter(townRoot)
	defer router.WaitPendingNotifications()
	fwd := &mail.Message{
		From:    "mayor/",
		To:      "overseer",
		Subject: fmt.Sprintf("[FWD][%s] HELP: %s", strings.ToUpper(string(assessment.Severity)), payload.Topic),
		Body: fmt.Sprintf("Forwarded from: %s\nAssessment: category=%s severity=%s (suggest → %s)\nRationale: %s\n\n%s",
			msg.From, assessment.Category, assessment.Severity, assessment.SuggestTo, assessment.Rationale, msg.Body),
		Priority: priority,
	}
	if err := router.Send(fwd); err != nil {
		return "", fmt.Errorf("forwarding to overseer: %w", err)
	}

	logCallback(townRoot, fmt.Sprintf("help_request: from %s: %s [%s/%s]",
		msg.From, payload.Topic, assessment.Category, assessment.Severity))

	return fmt.Sprintf("forwarded help request to overseer: %s [%s/%s]",
		payload.Topic, assessment.Category, assessment.Severity), nil
}

// HandleEscalation processes an ESCALATION: from a Witness.
func HandleEscalation(townRoot string, msg *mail.Message, dryRun bool) (string, error) {
	matches := PatternEscalation.FindStringSubmatch(msg.Subject)
	if len(matches) < 2 {
		return "", fmt.Errorf("could not parse topic from subject: %q", msg.Subject)
	}
	topic := matches[1]

	if dryRun {
		return fmt.Sprintf("would forward escalation to overseer: %s", topic), nil
	}

	router := mail.NewRouter(townRoot)
	defer router.WaitPendingNotifications()
	fwd := &mail.Message{
		From:     "mayor/",
		To:       "overseer",
		Subject:  fmt.Sprintf("[ESCALATION] %s", topic),
		Body:     fmt.Sprintf("Escalated by: %s\n\n%s", msg.From, msg.Body),
		Priority: mail.PriorityUrgent,
	}
	if err := router.Send(fwd); err != nil {
		return "", fmt.Errorf("forwarding escalation: %w", err)
	}

	logCallback(townRoot, fmt.Sprintf("escalation: from %s: %s", msg.From, topic))

	return fmt.Sprintf("forwarded escalation to overseer: %s", topic), nil
}

// HandleSling processes a SLING_REQUEST to spawn work on a polecat.
func HandleSling(townRoot string, msg *mail.Message, dryRun bool) (string, error) {
	matches := PatternSling.FindStringSubmatch(msg.Subject)
	if len(matches) < 2 {
		return "", fmt.Errorf("could not parse bead ID from subject: %q", msg.Subject)
	}
	beadID := matches[1]

	var targetRig string
	for _, line := range strings.Split(msg.Body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Rig:") {
			targetRig = strings.TrimSpace(strings.TrimPrefix(line, "Rig:"))
		}
	}

	if targetRig == "" {
		return "", fmt.Errorf("no target rig specified in sling request")
	}

	if dryRun {
		return fmt.Sprintf("would sling %s to %s", beadID, targetRig), nil
	}

	logCallback(townRoot, fmt.Sprintf("sling_request: bead %s to rig %s", beadID, targetRig))

	return fmt.Sprintf("logged sling request: %s to %s (execute with: gt sling %s %s)",
		beadID, targetRig, beadID, targetRig), nil
}

// ProcessSingleCallback handles a single callback message.
// Classifies the message, routes to the appropriate handler, and returns the result.
func ProcessSingleCallback(townRoot string, msg *mail.Message, dryRun bool) CallbackProcessResult {
	result := CallbackProcessResult{
		MessageID: msg.ID,
		From:      msg.From,
		Subject:   msg.Subject,
	}

	callbackType := ClassifyCallback(msg.Subject)
	result.CallbackType = callbackType

	var action string
	var err error

	switch callbackType {
	case CallbackTypePolecat:
		action, err = HandlePolecatDone(townRoot, msg, dryRun)
	case CallbackTypeMergeComplete:
		action, err = HandleMergeCompleted(townRoot, msg, dryRun)
	case CallbackTypeMergeRejected:
		action, err = HandleMergeRejected(townRoot, msg, dryRun)
	case CallbackTypeHelp:
		action, err = HandleHelp(townRoot, msg, dryRun)
	case CallbackTypeEscalation:
		action, err = HandleEscalation(townRoot, msg, dryRun)
	case CallbackTypeSling:
		action, err = HandleSling(townRoot, msg, dryRun)
	default:
		result.Action = "unknown message type, skipped"
		result.Handled = false
		return result
	}

	if err != nil {
		result.Handled = false
		result.Error = err.Error()
		return result
	}

	result.Handled = true
	result.Action = action

	// Archive handled messages (unless dry-run)
	if !dryRun {
		router := mail.NewRouter(townRoot)
		if mailbox, mErr := router.GetMailbox("mayor/"); mErr == nil {
			_ = mailbox.Delete(msg.ID)
		}
	}

	return result
}

// ProcessCallbacks is the main entry point for handling callbacks.
// It loads the mayor's mailbox, processes all pending callbacks,
// and persists the state. Called from the CLI command and the deacon patrol loop.
func ProcessCallbacks(townRoot string, dryRun bool) (*CallbacksResult, error) {
	result := &CallbacksResult{
		ProcessResults: []CallbackProcessResult{},
	}

	// Load current state to avoid re-processing
	state := LoadCallbackState(townRoot)

	// Access mayor's mailbox
	router := mail.NewRouter(townRoot)
	mailbox, err := router.GetMailbox("mayor/")
	if err != nil {
		return result, fmt.Errorf("accessing mayor mailbox: %w", err)
	}

	// List unread messages
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

		procResult := ProcessSingleCallback(townRoot, msg, dryRun)
		result.ProcessResults = append(result.ProcessResults, procResult)

		if procResult.Handled {
			result.Processed++
			state.ProcessedMessages[msg.ID] = time.Now().UTC()
		} else {
			result.Failed++
		}
	}

	// Persist updated state (unless dry-run)
	if !dryRun {
		if err := SaveCallbackState(townRoot, state); err != nil {
			style.PrintWarning("failed to save callbacks state: %v", err)
		}
	}

	result.Message = fmt.Sprintf("Processed %d callbacks (%d successful, %d failed, %d skipped)",
		len(messages), result.Processed, result.Failed, result.Skipped)

	return result, nil
}

// logCallback logs a callback processing event to the town log.
func logCallback(townRoot, context string) {
	logger := townlog.NewLogger(townRoot)
	_ = logger.Log(townlog.EventCallback, "mayor/", context)
}
