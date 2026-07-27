package cmd

import "testing"

func TestUploadDryRunAndSandboxFlagsAreMutuallyExclusive(t *testing.T) {
	dryRunFlag := uploadCmd.Flags().Lookup("dry-run")
	sandboxFlag := uploadCmd.Flags().Lookup("sandbox")
	originalDryRunChanged := dryRunFlag.Changed
	originalSandboxChanged := sandboxFlag.Changed
	originalDryRun := flagDryRun
	originalSandbox := flagSandbox
	defer func() {
		flagDryRun = originalDryRun
		flagSandbox = originalSandbox
		dryRunFlag.Changed = originalDryRunChanged
		sandboxFlag.Changed = originalSandboxChanged
	}()

	if err := uploadCmd.Flags().Set("dry-run", "true"); err != nil {
		t.Fatal(err)
	}
	if err := uploadCmd.Flags().Set("sandbox", "true"); err != nil {
		t.Fatal(err)
	}
	if err := uploadCmd.ValidateFlagGroups(); err == nil {
		t.Fatal("expected mutually exclusive flag error")
	}
}
