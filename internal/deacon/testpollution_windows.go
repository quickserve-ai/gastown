//go:build windows

package deacon

// TestPollutionResult holds counts from a test pollution cleanup run.
type TestPollutionResult struct {
	RogueDoltKilled      int
	StaleTestDirsRemoved int
	StalePIDFilesRemoved int
	DeadWorktreesPruned  int
}

// CleanTestPollution is a no-op on Windows.
func CleanTestPollution(townRoot string) (TestPollutionResult, error) {
	return TestPollutionResult{}, nil
}
