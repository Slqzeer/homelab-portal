package e2e_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLauncherFailsClosedAndRetainsNonSecretReport(t *testing.T) {
	shell, err := exec.LookPath("pwsh")
	if err != nil {
		shell, err = exec.LookPath("powershell")
	}
	if err != nil {
		t.Skip("PowerShell unavailable; configuration/HTTP contracts still run")
	}
	reportPath := filepath.Join(t.TempDir(), "acceptance.json")
	script, err := filepath.Abs(filepath.Join("..", "..", "scripts", "verify-cluster.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(shell, "-NoProfile", "-NonInteractive", "-File", script, "-ReportPath", reportPath)
	for _, env := range os.Environ() {
		key := strings.ToUpper(strings.SplitN(env, "=", 2)[0])
		if !strings.HasPrefix(key, "ACCEPT_") && key != "KUBECONFIG" {
			cmd.Env = append(cmd.Env, env)
		}
	}
	cmd.Env = append(cmd.Env, "ACCEPT_ADMIN_COOKIE=never-print-this-cookie")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("launcher accepted absent disposable configuration")
	}
	if strings.Contains(string(out), "never-print-this-cookie") {
		t.Fatal("launcher disclosed a credential")
	}
	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal("failed launch did not retain its report")
	}
	var report struct {
		Passed, LiveAttempted bool
		Results               []struct{ Case string }
	}
	if json.Unmarshal(data, &report) != nil || report.Passed || report.LiveAttempted || len(report.Results) == 0 {
		t.Fatal("failed launch report is not fail closed")
	}
	if strings.Contains(string(data), "never-print-this-cookie") {
		t.Fatal("report disclosed a credential")
	}
}
