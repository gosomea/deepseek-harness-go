package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFixture(t *testing.T, root, name, body string) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestExportGateRejectsMissingComments(t *testing.T) {
	root := t.TempDir()
	for _, tc := range []struct {
		body    string
		missing bool
	}{
		{"package example\nfunc Public(){}\n", true},
		{"package example\n// Public has documentation.\nfunc Public(){}\n", false},
		{"package example\n// Public is a record.\ntype Public struct { Field string }\n", true},
		{"package example\n// Public is a record.\ntype Public struct {\n// Field is documented.\nField string\n}\n", false},
		{"package example\nvar Public = 1\n", true},
	} {
		path := writeFixture(t, root, "source.go", tc.body)
		if (checkExports(path) != nil) != tc.missing {
			t.Fatal(tc.body)
		}
	}
}

func TestLinkGateRejectsMissingTargetsAndFragments(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "target.md", "# 中文 标题\n")
	for _, tc := range []struct {
		link string
		bad  bool
	}{
		{"[ok](target.md#中文-标题)", false}, {"[bad](missing.md)", true},
		{"[bad](target.md#missing)", true}, {"[bad](../outside.md)", true},
		{"[web](https://example.com)", false}, {"```md\n[example](missing.md)\n```", false},
		{"`NewKey[T](\"model\")`", false},
	} {
		path := writeFixture(t, root, "README.md", tc.link)
		if (checkLinks(root, path) != nil) != tc.bad {
			t.Fatal(tc.link)
		}
	}
}

func TestDocumentationGateDetectsREADMEAndAPIDrift(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "cordis/doc.go", "// Package cordis is a fixture.\npackage cordis\n// Public is documented.\nfunc Public(){}\n")
	api, err := renderAPI(root)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, root, "docs/cordis/api.md", string(api))
	writeFixture(t, root, "docs/cordis/lifecycle.md", "# Lifecycle\n")
	writeFixture(t, root, "docs/cordis/services.md", "# Services\n")
	writeFixture(t, root, "docs/cordis/events.md", "# Events\n")
	writeFixture(t, root, "cordis/fiber.go", "package cordis\n")
	writeFixture(t, root, "cordis/plugin.go", "package cordis\n")
	if err := checkDocs(root); err == nil || !strings.Contains(err.Error(), "needs README") {
		t.Fatal(err)
	}
	writeFixture(t, root, "cordis/README.md", fixtureREADME("package-library"))
	if err := checkDocs(root); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, root, "docs/cordis/api.md", "stale\n")
	if err := checkDocs(root); err == nil || !strings.Contains(err.Error(), "API stale") {
		t.Fatal(err)
	}
}

// TestCoverageGateChecksEveryGatedPackage pins both directions of the coverage
// gate for every gated runtime package: a passing profile, a profile below the
// threshold, and a profile that omits the package entirely.
//
// Each newly gated package adds three cases, so registering a package in
// coverageRequirement without extending this fixture fails the gate. That is
// the intended behaviour: the fixture is the gate's own test, and a gate whose
// test does not cover its list would silently stop covering the new entries.
func TestCoverageGateChecksEveryGatedPackage(t *testing.T) {
	root := t.TempDir()
	for _, tc := range []struct {
		name    string
		profile string
		bad     bool
	}{
		{"empty-profile", "mode: atomic\n", true},
		{"all-gated-packages-pass", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmPass + scopePass + sessionPass + toolsPass + systempromptPass + llmtrpcPass + agentPass + agentloopPass + persistencePass + sessionjsonlPass + workspacePass + capfsPass + subprocessPass + sandboxPass + approvalPass + localPass + contextPass + settingsPass + presetPass + skillsPass + attachmentPass + attachmentPluginPass + spillPass + spillPluginPass + compactionPass + compactionPluginPass + subagentCapPass + subagentPluginPass + taskstatePass + jobsCapPass + schedulePass + workflowCapPass + workflowPluginPass + protocolPass + hostapiPass + sdkPass + mcpPass + lspPass + sshPass + hooksPass + webPass + clientviewPass + desktophostPass + conformancePass + telemetryPass, false},
		{"cordis-below-threshold", "mode: atomic\n" + cordisFail + testkitPass + loaderPass, true},
		{"testkit-below-threshold", "mode: atomic\n" + cordisPass + testkitFail + loaderPass, true},
		{"loader-below-threshold", "mode: atomic\n" + cordisPass + testkitPass + loaderFail + appPass, true},
		{"app-below-threshold", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appFail + llmPass + scopePass, true},
		{"scope-below-threshold", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmPass + scopeFail + sessionPass, true},
		{"session-below-threshold", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmPass + scopePass + sessionFail, true},
		{"llm-below-threshold", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmFail, true},
		{"testkit-missing", "mode: atomic\n" + cordisPass + loaderPass + appPass, true},
		{"cordis-missing", "mode: atomic\n" + testkitPass + loaderPass + appPass, true},
		{"loader-missing", "mode: atomic\n" + cordisPass + testkitPass + appPass, true},
		{"app-missing", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + llmPass + scopePass, true},
		{"scope-missing", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmPass + sessionPass, true},
		{"session-missing", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmPass + scopePass, true},
		{"llm-missing", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass, true},
		{"tools-below-threshold", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmPass + scopePass + sessionPass + toolsFail, true},
		{"systemprompt-below-threshold", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmPass + scopePass + sessionPass + systempromptFail, true},
		{"llmtrpc-below-threshold", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmPass + scopePass + sessionPass + llmtrpcFail, true},
		{"agent-below-threshold", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmPass + scopePass + sessionPass + toolsPass + systempromptPass + llmtrpcPass + agentFail, true},
		{"agentloop-below-threshold", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmPass + scopePass + sessionPass + toolsPass + systempromptPass + llmtrpcPass + agentPass + agentloopFail, true},
		{"persistence-below-threshold", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmPass + scopePass + sessionPass + toolsPass + systempromptPass + llmtrpcPass + agentPass + agentloopPass + persistenceFail, true},
		{"sessionjsonl-below-threshold", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmPass + scopePass + sessionPass + toolsPass + systempromptPass + llmtrpcPass + agentPass + agentloopPass + persistencePass + sessionjsonlFail, true},
		{"tools-missing", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmPass + scopePass + sessionPass, true},
		{"systemprompt-missing", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmPass + scopePass + sessionPass, true},
		{"llmtrpc-missing", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmPass + scopePass + sessionPass, true},
		{"agent-missing", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmPass + scopePass + sessionPass + toolsPass + systempromptPass + llmtrpcPass, true},
		{"agentloop-missing", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmPass + scopePass + sessionPass + toolsPass + systempromptPass + llmtrpcPass + agentPass, true},
		{"persistence-missing", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmPass + scopePass + sessionPass + toolsPass + systempromptPass + llmtrpcPass + agentPass + agentloopPass, true},
		{"sessionjsonl-missing", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmPass + scopePass + sessionPass + toolsPass + systempromptPass + llmtrpcPass + agentPass + agentloopPass + persistencePass, true},
		{"workspace-below-threshold", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmPass + scopePass + sessionPass + toolsPass + systempromptPass + llmtrpcPass + agentPass + agentloopPass + persistencePass + sessionjsonlPass + workspaceFail, true},
		{"capfs-below-threshold", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmPass + scopePass + sessionPass + toolsPass + systempromptPass + llmtrpcPass + agentPass + agentloopPass + persistencePass + sessionjsonlPass + workspacePass + capfsFail, true},
		{"subprocess-below-threshold", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmPass + scopePass + sessionPass + toolsPass + systempromptPass + llmtrpcPass + agentPass + agentloopPass + persistencePass + sessionjsonlPass + workspacePass + capfsPass + subprocessFail, true},
		{"sandbox-below-threshold", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmPass + scopePass + sessionPass + toolsPass + systempromptPass + llmtrpcPass + agentPass + agentloopPass + persistencePass + sessionjsonlPass + workspacePass + capfsPass + subprocessPass + sandboxFail, true},
		{"approval-below-threshold", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmPass + scopePass + sessionPass + toolsPass + systempromptPass + llmtrpcPass + agentPass + agentloopPass + persistencePass + sessionjsonlPass + workspacePass + capfsPass + subprocessPass + sandboxPass + approvalFail, true},
		{"local-below-threshold", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmPass + scopePass + sessionPass + toolsPass + systempromptPass + llmtrpcPass + agentPass + agentloopPass + persistencePass + sessionjsonlPass + workspacePass + capfsPass + subprocessPass + sandboxPass + approvalPass + localFail, true},
		{"context-below-threshold", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmPass + scopePass + sessionPass + toolsPass + systempromptPass + llmtrpcPass + agentPass + agentloopPass + persistencePass + sessionjsonlPass + workspacePass + capfsPass + subprocessPass + sandboxPass + approvalPass + localPass + contextFail, true},
		{"settings-below-threshold", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmPass + scopePass + sessionPass + toolsPass + systempromptPass + llmtrpcPass + agentPass + agentloopPass + persistencePass + sessionjsonlPass + workspacePass + capfsPass + subprocessPass + sandboxPass + approvalPass + localPass + contextPass + settingsFail, true},
		{"preset-below-threshold", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmPass + scopePass + sessionPass + toolsPass + systempromptPass + llmtrpcPass + agentPass + agentloopPass + persistencePass + sessionjsonlPass + workspacePass + capfsPass + subprocessPass + sandboxPass + approvalPass + localPass + contextPass + settingsPass + presetFail, true},
		{"skills-below-threshold", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmPass + scopePass + sessionPass + toolsPass + systempromptPass + llmtrpcPass + agentPass + agentloopPass + persistencePass + sessionjsonlPass + workspacePass + capfsPass + subprocessPass + sandboxPass + approvalPass + localPass + contextPass + settingsPass + presetPass + skillsFail, true},
		{"attachment-below-threshold", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmPass + scopePass + sessionPass + toolsPass + systempromptPass + llmtrpcPass + agentPass + agentloopPass + persistencePass + sessionjsonlPass + workspacePass + capfsPass + subprocessPass + sandboxPass + approvalPass + localPass + contextPass + settingsPass + presetPass + skillsPass + attachmentFail, true},
		{"attachment-plugin-below-threshold", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmPass + scopePass + sessionPass + toolsPass + systempromptPass + llmtrpcPass + agentPass + agentloopPass + persistencePass + sessionjsonlPass + workspacePass + capfsPass + subprocessPass + sandboxPass + approvalPass + localPass + contextPass + settingsPass + presetPass + skillsPass + attachmentPass + attachmentPluginFail, true},
		{"spill-below-threshold", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmPass + scopePass + sessionPass + toolsPass + systempromptPass + llmtrpcPass + agentPass + agentloopPass + persistencePass + sessionjsonlPass + workspacePass + capfsPass + subprocessPass + sandboxPass + approvalPass + localPass + contextPass + settingsPass + presetPass + skillsPass + attachmentPass + attachmentPluginPass + spillFail, true},
		{"spill-plugin-below-threshold", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmPass + scopePass + sessionPass + toolsPass + systempromptPass + llmtrpcPass + agentPass + agentloopPass + persistencePass + sessionjsonlPass + workspacePass + capfsPass + subprocessPass + sandboxPass + approvalPass + localPass + contextPass + settingsPass + presetPass + skillsPass + attachmentPass + attachmentPluginPass + spillPass + spillPluginFail, true},
		{"compaction-below-threshold", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmPass + scopePass + sessionPass + toolsPass + systempromptPass + llmtrpcPass + agentPass + agentloopPass + persistencePass + sessionjsonlPass + workspacePass + capfsPass + subprocessPass + sandboxPass + approvalPass + localPass + contextPass + settingsPass + presetPass + skillsPass + attachmentPass + attachmentPluginPass + spillPass + spillPluginPass + compactionFail, true},
		{"compaction-plugin-below-threshold", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmPass + scopePass + sessionPass + toolsPass + systempromptPass + llmtrpcPass + agentPass + agentloopPass + persistencePass + sessionjsonlPass + workspacePass + capfsPass + subprocessPass + sandboxPass + approvalPass + localPass + contextPass + settingsPass + presetPass + skillsPass + attachmentPass + attachmentPluginPass + spillPass + spillPluginPass + compactionPass + compactionPluginFail, true},
		{"workspace-missing", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmPass + scopePass + sessionPass + toolsPass + systempromptPass + llmtrpcPass + agentPass + agentloopPass + persistencePass + sessionjsonlPass + " + " + capfsPass + subprocessPass + sandboxPass + approvalPass + localPass + contextPass, true},
		{"capfs-missing", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmPass + scopePass + sessionPass + toolsPass + systempromptPass + llmtrpcPass + agentPass + agentloopPass + persistencePass + sessionjsonlPass + " + " + workspacePass + subprocessPass + sandboxPass + approvalPass + localPass, true},
		{"subprocess-missing", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmPass + scopePass + sessionPass + toolsPass + systempromptPass + llmtrpcPass + agentPass + agentloopPass + persistencePass + sessionjsonlPass + " + " + workspacePass + capfsPass + sandboxPass + approvalPass + localPass, true},
		{"sandbox-missing", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmPass + scopePass + sessionPass + toolsPass + systempromptPass + llmtrpcPass + agentPass + agentloopPass + persistencePass + sessionjsonlPass + " + " + workspacePass + capfsPass + subprocessPass + approvalPass + localPass, true},
		{"approval-missing", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmPass + scopePass + sessionPass + toolsPass + systempromptPass + llmtrpcPass + agentPass + agentloopPass + persistencePass + sessionjsonlPass + " + " + workspacePass + capfsPass + subprocessPass + sandboxPass + localPass, true},
		{"local-missing", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmPass + scopePass + sessionPass + toolsPass + systempromptPass + llmtrpcPass + agentPass + agentloopPass + persistencePass + sessionjsonlPass + " + " + workspacePass + capfsPass + subprocessPass + sandboxPass + approvalPass + contextPass, true},
		{"context-missing", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmPass + scopePass + sessionPass + toolsPass + systempromptPass + llmtrpcPass + agentPass + agentloopPass + persistencePass + sessionjsonlPass + workspacePass + capfsPass + subprocessPass + sandboxPass + approvalPass + localPass + settingsPass + presetPass, true},
		{"settings-missing", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmPass + scopePass + sessionPass + toolsPass + systempromptPass + llmtrpcPass + agentPass + agentloopPass + persistencePass + sessionjsonlPass + workspacePass + capfsPass + subprocessPass + sandboxPass + approvalPass + localPass + contextPass + presetPass, true},
		{"preset-missing", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmPass + scopePass + sessionPass + toolsPass + systempromptPass + llmtrpcPass + agentPass + agentloopPass + persistencePass + sessionjsonlPass + workspacePass + capfsPass + subprocessPass + sandboxPass + approvalPass + localPass + contextPass + settingsPass + skillsPass, true},
		{"skills-missing", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmPass + scopePass + sessionPass + toolsPass + systempromptPass + llmtrpcPass + agentPass + agentloopPass + persistencePass + sessionjsonlPass + workspacePass + capfsPass + subprocessPass + sandboxPass + approvalPass + localPass + contextPass + settingsPass + presetPass + attachmentPass + attachmentPluginPass, true},
		{"attachment-missing", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmPass + scopePass + sessionPass + toolsPass + systempromptPass + llmtrpcPass + agentPass + agentloopPass + persistencePass + sessionjsonlPass + workspacePass + capfsPass + subprocessPass + sandboxPass + approvalPass + localPass + contextPass + settingsPass + presetPass + skillsPass + attachmentPluginPass, true},
		{"attachment-plugin-missing", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmPass + scopePass + sessionPass + toolsPass + systempromptPass + llmtrpcPass + agentPass + agentloopPass + persistencePass + sessionjsonlPass + workspacePass + capfsPass + subprocessPass + sandboxPass + approvalPass + localPass + contextPass + settingsPass + presetPass + skillsPass + attachmentPass + spillPass + spillPluginPass, true},
		{"spill-missing", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmPass + scopePass + sessionPass + toolsPass + systempromptPass + llmtrpcPass + agentPass + agentloopPass + persistencePass + sessionjsonlPass + workspacePass + capfsPass + subprocessPass + sandboxPass + approvalPass + localPass + contextPass + settingsPass + presetPass + skillsPass + attachmentPass + attachmentPluginPass + spillPluginPass, true},
		{"spill-plugin-missing", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmPass + scopePass + sessionPass + toolsPass + systempromptPass + llmtrpcPass + agentPass + agentloopPass + persistencePass + sessionjsonlPass + workspacePass + capfsPass + subprocessPass + sandboxPass + approvalPass + localPass + contextPass + settingsPass + presetPass + skillsPass + attachmentPass + attachmentPluginPass + spillPass + compactionPass + compactionPluginPass, true},
		{"compaction-missing", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmPass + scopePass + sessionPass + toolsPass + systempromptPass + llmtrpcPass + agentPass + agentloopPass + persistencePass + sessionjsonlPass + workspacePass + capfsPass + subprocessPass + sandboxPass + approvalPass + localPass + contextPass + settingsPass + presetPass + skillsPass + attachmentPass + attachmentPluginPass + spillPass + spillPluginPass + compactionPluginPass, true},
		{"compaction-plugin-missing", "mode: atomic\n" + cordisPass + testkitPass + loaderPass + appPass + llmPass + scopePass + sessionPass + toolsPass + systempromptPass + llmtrpcPass + agentPass + agentloopPass + persistencePass + sessionjsonlPass + workspacePass + capfsPass + subprocessPass + sandboxPass + approvalPass + localPass + contextPass + settingsPass + presetPass + skillsPass + attachmentPass + attachmentPluginPass + spillPass + spillPluginPass + compactionPass, true},
		{"unrelated-package-only", "mode: atomic\ngithub.com/gosomea/deepseek-harness-go/other/a.go:1.1,2.1 9 1\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeFixture(t, root, "coverage.out", tc.profile)
			if (checkCoverage(path) != nil) != tc.bad {
				t.Fatalf("profile %q: bad = %v", tc.profile, tc.bad)
			}
		})
	}
}

const cordisPass = "github.com/gosomea/deepseek-harness-go/cordis/a.go:1.1,2.1 9 1\ngithub.com/gosomea/deepseek-harness-go/cordis/a.go:3.1,4.1 1 0\n"
const cordisFail = "github.com/gosomea/deepseek-harness-go/cordis/a.go:1.1,2.1 8 1\ngithub.com/gosomea/deepseek-harness-go/cordis/a.go:3.1,4.1 2 0\n"
const testkitPass = "github.com/gosomea/deepseek-harness-go/internal/testkit/a.go:1.1,2.1 9 1\ngithub.com/gosomea/deepseek-harness-go/internal/testkit/a.go:3.1,4.1 1 0\n"
const testkitFail = "github.com/gosomea/deepseek-harness-go/internal/testkit/a.go:1.1,2.1 8 1\ngithub.com/gosomea/deepseek-harness-go/internal/testkit/a.go:3.1,4.1 2 0\n"
const loaderPass = "github.com/gosomea/deepseek-harness-go/loader/a.go:1.1,2.1 9 1\ngithub.com/gosomea/deepseek-harness-go/loader/a.go:3.1,4.1 1 0\n"
const loaderFail = "github.com/gosomea/deepseek-harness-go/loader/a.go:1.1,2.1 8 1\ngithub.com/gosomea/deepseek-harness-go/loader/a.go:3.1,4.1 2 0\n"
const appPass = "github.com/gosomea/deepseek-harness-go/app/a.go:1.1,2.1 9 1\ngithub.com/gosomea/deepseek-harness-go/app/a.go:3.1,4.1 1 0\n"
const appFail = "github.com/gosomea/deepseek-harness-go/app/a.go:1.1,2.1 8 1\ngithub.com/gosomea/deepseek-harness-go/app/a.go:3.1,4.1 2 0\n"
const llmPass = "github.com/gosomea/deepseek-harness-go/llm/a.go:1.1,2.1 9 1\ngithub.com/gosomea/deepseek-harness-go/llm/a.go:3.1,4.1 1 0\n"
const llmFail = "github.com/gosomea/deepseek-harness-go/llm/a.go:1.1,2.1 8 1\ngithub.com/gosomea/deepseek-harness-go/llm/a.go:3.1,4.1 2 0\n"
const scopePass = "github.com/gosomea/deepseek-harness-go/scope/a.go:1.1,2.1 9 1\ngithub.com/gosomea/deepseek-harness-go/scope/a.go:3.1,4.1 1 0\n"
const scopeFail = "github.com/gosomea/deepseek-harness-go/scope/a.go:1.1,2.1 8 1\ngithub.com/gosomea/deepseek-harness-go/scope/a.go:3.1,4.1 2 0\n"
const sessionPass = "github.com/gosomea/deepseek-harness-go/session/a.go:1.1,2.1 9 1\ngithub.com/gosomea/deepseek-harness-go/session/a.go:3.1,4.1 1 0\n"
const sessionFail = "github.com/gosomea/deepseek-harness-go/session/a.go:1.1,2.1 8 1\ngithub.com/gosomea/deepseek-harness-go/session/a.go:3.1,4.1 2 0\n"
const toolsPass = "github.com/gosomea/deepseek-harness-go/tools/a.go:1.1,2.1 9 1\ngithub.com/gosomea/deepseek-harness-go/tools/a.go:3.1,4.1 1 0\n"
const toolsFail = "github.com/gosomea/deepseek-harness-go/tools/a.go:1.1,2.1 8 1\ngithub.com/gosomea/deepseek-harness-go/tools/a.go:3.1,4.1 2 0\n"
const systempromptPass = "github.com/gosomea/deepseek-harness-go/systemprompt/a.go:1.1,2.1 9 1\ngithub.com/gosomea/deepseek-harness-go/systemprompt/a.go:3.1,4.1 1 0\n"
const systempromptFail = "github.com/gosomea/deepseek-harness-go/systemprompt/a.go:1.1,2.1 8 1\ngithub.com/gosomea/deepseek-harness-go/systemprompt/a.go:3.1,4.1 2 0\n"
const llmtrpcPass = "github.com/gosomea/deepseek-harness-go/plugins/llmtrpc/a.go:1.1,2.1 9 1\ngithub.com/gosomea/deepseek-harness-go/plugins/llmtrpc/a.go:3.1,4.1 1 0\n"
const llmtrpcFail = "github.com/gosomea/deepseek-harness-go/plugins/llmtrpc/a.go:1.1,2.1 8 1\ngithub.com/gosomea/deepseek-harness-go/plugins/llmtrpc/a.go:3.1,4.1 2 0\n"
const agentPass = "github.com/gosomea/deepseek-harness-go/agent/a.go:1.1,2.1 9 1\ngithub.com/gosomea/deepseek-harness-go/agent/a.go:3.1,4.1 1 0\n"
const agentFail = "github.com/gosomea/deepseek-harness-go/agent/a.go:1.1,2.1 8 1\ngithub.com/gosomea/deepseek-harness-go/agent/a.go:3.1,4.1 2 0\n"
const agentloopPass = "github.com/gosomea/deepseek-harness-go/agentloop/a.go:1.1,2.1 9 1\ngithub.com/gosomea/deepseek-harness-go/agentloop/a.go:3.1,4.1 1 0\n"
const agentloopFail = "github.com/gosomea/deepseek-harness-go/agentloop/a.go:1.1,2.1 8 1\ngithub.com/gosomea/deepseek-harness-go/agentloop/a.go:3.1,4.1 2 0\n"
const persistencePass = "github.com/gosomea/deepseek-harness-go/persistence/a.go:1.1,2.1 9 1\ngithub.com/gosomea/deepseek-harness-go/persistence/a.go:3.1,4.1 1 0\n"
const persistenceFail = "github.com/gosomea/deepseek-harness-go/persistence/a.go:1.1,2.1 8 1\ngithub.com/gosomea/deepseek-harness-go/persistence/a.go:3.1,4.1 2 0\n"
const sessionjsonlPass = "github.com/gosomea/deepseek-harness-go/plugins/sessionjsonl/a.go:1.1,2.1 9 1\ngithub.com/gosomea/deepseek-harness-go/plugins/sessionjsonl/a.go:3.1,4.1 1 0\n"
const sessionjsonlFail = "github.com/gosomea/deepseek-harness-go/plugins/sessionjsonl/a.go:1.1,2.1 8 1\ngithub.com/gosomea/deepseek-harness-go/plugins/sessionjsonl/a.go:3.1,4.1 2 0\n"
const workspacePass = "github.com/gosomea/deepseek-harness-go/capability/workspace/a.go:1.1,2.1 9 1\ngithub.com/gosomea/deepseek-harness-go/capability/workspace/a.go:3.1,4.1 1 0\n"
const workspaceFail = "github.com/gosomea/deepseek-harness-go/capability/workspace/a.go:1.1,2.1 8 1\ngithub.com/gosomea/deepseek-harness-go/capability/workspace/a.go:3.1,4.1 2 0\n"
const capfsPass = "github.com/gosomea/deepseek-harness-go/capability/fs/a.go:1.1,2.1 9 1\ngithub.com/gosomea/deepseek-harness-go/capability/fs/a.go:3.1,4.1 1 0\n"
const capfsFail = "github.com/gosomea/deepseek-harness-go/capability/fs/a.go:1.1,2.1 8 1\ngithub.com/gosomea/deepseek-harness-go/capability/fs/a.go:3.1,4.1 2 0\n"
const subprocessPass = "github.com/gosomea/deepseek-harness-go/capability/subprocess/a.go:1.1,2.1 9 1\ngithub.com/gosomea/deepseek-harness-go/capability/subprocess/a.go:3.1,4.1 1 0\n"
const subprocessFail = "github.com/gosomea/deepseek-harness-go/capability/subprocess/a.go:1.1,2.1 8 1\ngithub.com/gosomea/deepseek-harness-go/capability/subprocess/a.go:3.1,4.1 2 0\n"
const sandboxPass = "github.com/gosomea/deepseek-harness-go/capability/sandbox/a.go:1.1,2.1 9 1\ngithub.com/gosomea/deepseek-harness-go/capability/sandbox/a.go:3.1,4.1 1 0\n"
const sandboxFail = "github.com/gosomea/deepseek-harness-go/capability/sandbox/a.go:1.1,2.1 8 1\ngithub.com/gosomea/deepseek-harness-go/capability/sandbox/a.go:3.1,4.1 2 0\n"
const approvalPass = "github.com/gosomea/deepseek-harness-go/capability/approval/a.go:1.1,2.1 9 1\ngithub.com/gosomea/deepseek-harness-go/capability/approval/a.go:3.1,4.1 1 0\n"
const approvalFail = "github.com/gosomea/deepseek-harness-go/capability/approval/a.go:1.1,2.1 8 1\ngithub.com/gosomea/deepseek-harness-go/capability/approval/a.go:3.1,4.1 2 0\n"
const localPass = "github.com/gosomea/deepseek-harness-go/plugins/local/a.go:1.1,2.1 9 1\ngithub.com/gosomea/deepseek-harness-go/plugins/local/a.go:3.1,4.1 1 0\n"
const contextPass = "github.com/gosomea/deepseek-harness-go/plugins/context/a.go:1.1,2.1 9 1\ngithub.com/gosomea/deepseek-harness-go/plugins/context/a.go:3.1,4.1 1 0\n"
const contextFail = "github.com/gosomea/deepseek-harness-go/plugins/context/a.go:1.1,2.1 8 1\ngithub.com/gosomea/deepseek-harness-go/plugins/context/a.go:3.1,4.1 2 0\n"
const settingsPass = "github.com/gosomea/deepseek-harness-go/plugins/settings/a.go:1.1,2.1 9 1\ngithub.com/gosomea/deepseek-harness-go/plugins/settings/a.go:3.1,4.1 1 0\n"
const settingsFail = "github.com/gosomea/deepseek-harness-go/plugins/settings/a.go:1.1,2.1 8 1\ngithub.com/gosomea/deepseek-harness-go/plugins/settings/a.go:3.1,4.1 2 0\n"
const presetPass = "github.com/gosomea/deepseek-harness-go/plugins/preset/a.go:1.1,2.1 9 1\ngithub.com/gosomea/deepseek-harness-go/plugins/preset/a.go:3.1,4.1 1 0\n"
const presetFail = "github.com/gosomea/deepseek-harness-go/plugins/preset/a.go:1.1,2.1 8 1\ngithub.com/gosomea/deepseek-harness-go/plugins/preset/a.go:3.1,4.1 2 0\n"
const skillsPass = "github.com/gosomea/deepseek-harness-go/plugins/skills/a.go:1.1,2.1 9 1\ngithub.com/gosomea/deepseek-harness-go/plugins/skills/a.go:3.1,4.1 1 0\n"
const skillsFail = "github.com/gosomea/deepseek-harness-go/plugins/skills/a.go:1.1,2.1 8 1\ngithub.com/gosomea/deepseek-harness-go/plugins/skills/a.go:3.1,4.1 2 0\n"
const attachmentPass = "github.com/gosomea/deepseek-harness-go/capability/attachment/a.go:1.1,2.1 9 1\ngithub.com/gosomea/deepseek-harness-go/capability/attachment/a.go:3.1,4.1 1 0\n"
const attachmentFail = "github.com/gosomea/deepseek-harness-go/capability/attachment/a.go:1.1,2.1 8 1\ngithub.com/gosomea/deepseek-harness-go/capability/attachment/a.go:3.1,4.1 2 0\n"
const attachmentPluginPass = "github.com/gosomea/deepseek-harness-go/plugins/attachment/a.go:1.1,2.1 9 1\ngithub.com/gosomea/deepseek-harness-go/plugins/attachment/a.go:3.1,4.1 1 0\n"
const attachmentPluginFail = "github.com/gosomea/deepseek-harness-go/plugins/attachment/a.go:1.1,2.1 8 1\ngithub.com/gosomea/deepseek-harness-go/plugins/attachment/a.go:3.1,4.1 2 0\n"
const spillPass = "github.com/gosomea/deepseek-harness-go/capability/spill/a.go:1.1,2.1 9 1\ngithub.com/gosomea/deepseek-harness-go/capability/spill/a.go:3.1,4.1 1 0\n"
const spillFail = "github.com/gosomea/deepseek-harness-go/capability/spill/a.go:1.1,2.1 8 1\ngithub.com/gosomea/deepseek-harness-go/capability/spill/a.go:3.1,4.1 2 0\n"
const spillPluginPass = "github.com/gosomea/deepseek-harness-go/plugins/spill/a.go:1.1,2.1 9 1\ngithub.com/gosomea/deepseek-harness-go/plugins/spill/a.go:3.1,4.1 1 0\n"
const spillPluginFail = "github.com/gosomea/deepseek-harness-go/plugins/spill/a.go:1.1,2.1 8 1\ngithub.com/gosomea/deepseek-harness-go/plugins/spill/a.go:3.1,4.1 2 0\n"
const compactionPass = "github.com/gosomea/deepseek-harness-go/capability/compaction/a.go:1.1,2.1 9 1\ngithub.com/gosomea/deepseek-harness-go/capability/compaction/a.go:3.1,4.1 1 0\n"
const compactionFail = "github.com/gosomea/deepseek-harness-go/capability/compaction/a.go:1.1,2.1 8 1\ngithub.com/gosomea/deepseek-harness-go/capability/compaction/a.go:3.1,4.1 2 0\n"
const compactionPluginPass = "github.com/gosomea/deepseek-harness-go/plugins/compaction/a.go:1.1,2.1 9 1\ngithub.com/gosomea/deepseek-harness-go/plugins/compaction/a.go:3.1,4.1 1 0\n"
const compactionPluginFail = "github.com/gosomea/deepseek-harness-go/plugins/compaction/a.go:1.1,2.1 8 1\ngithub.com/gosomea/deepseek-harness-go/plugins/compaction/a.go:3.1,4.1 2 0\n"
const localFail = "github.com/gosomea/deepseek-harness-go/plugins/local/a.go:1.1,2.1 8 1\ngithub.com/gosomea/deepseek-harness-go/plugins/local/a.go:3.1,4.1 2 0\n"

const subagentCapPass = "github.com/gosomea/deepseek-harness-go/capability/subagent/a.go:1.1,2.1 9 1\ngithub.com/gosomea/deepseek-harness-go/capability/subagent/a.go:3.1,4.1 1 0\n"
const subagentCapFail = "github.com/gosomea/deepseek-harness-go/capability/subagent/a.go:1.1,2.1 8 1\ngithub.com/gosomea/deepseek-harness-go/capability/subagent/a.go:3.1,4.1 2 0\n"
const subagentPluginPass = "github.com/gosomea/deepseek-harness-go/plugins/subagent/a.go:1.1,2.1 9 1\ngithub.com/gosomea/deepseek-harness-go/plugins/subagent/a.go:3.1,4.1 1 0\n"
const subagentPluginFail = "github.com/gosomea/deepseek-harness-go/plugins/subagent/a.go:1.1,2.1 8 1\ngithub.com/gosomea/deepseek-harness-go/plugins/subagent/a.go:3.1,4.1 2 0\n"
const taskstatePass = "github.com/gosomea/deepseek-harness-go/capability/taskstate/a.go:1.1,2.1 9 1\ngithub.com/gosomea/deepseek-harness-go/capability/taskstate/a.go:3.1,4.1 1 0\n"
const taskstateFail = "github.com/gosomea/deepseek-harness-go/capability/taskstate/a.go:1.1,2.1 8 1\ngithub.com/gosomea/deepseek-harness-go/capability/taskstate/a.go:3.1,4.1 2 0\n"
const jobsCapPass = "github.com/gosomea/deepseek-harness-go/capability/jobs/a.go:1.1,2.1 9 1\ngithub.com/gosomea/deepseek-harness-go/capability/jobs/a.go:3.1,4.1 1 0\n"
const jobsCapFail = "github.com/gosomea/deepseek-harness-go/capability/jobs/a.go:1.1,2.1 8 1\ngithub.com/gosomea/deepseek-harness-go/capability/jobs/a.go:3.1,4.1 2 0\n"
const schedulePass = "github.com/gosomea/deepseek-harness-go/plugins/schedule/a.go:1.1,2.1 9 1\ngithub.com/gosomea/deepseek-harness-go/plugins/schedule/a.go:3.1,4.1 1 0\n"
const scheduleFail = "github.com/gosomea/deepseek-harness-go/plugins/schedule/a.go:1.1,2.1 8 1\ngithub.com/gosomea/deepseek-harness-go/plugins/schedule/a.go:3.1,4.1 2 0\n"
const workflowCapPass = "github.com/gosomea/deepseek-harness-go/capability/workflow/a.go:1.1,2.1 9 1\ngithub.com/gosomea/deepseek-harness-go/capability/workflow/a.go:3.1,4.1 1 0\n"
const workflowCapFail = "github.com/gosomea/deepseek-harness-go/capability/workflow/a.go:1.1,2.1 8 1\ngithub.com/gosomea/deepseek-harness-go/capability/workflow/a.go:3.1,4.1 2 0\n"
const workflowPluginPass = "github.com/gosomea/deepseek-harness-go/plugins/workflow/a.go:1.1,2.1 9 1\ngithub.com/gosomea/deepseek-harness-go/plugins/workflow/a.go:3.1,4.1 1 0\n"
const workflowPluginFail = "github.com/gosomea/deepseek-harness-go/plugins/workflow/a.go:1.1,2.1 8 1\ngithub.com/gosomea/deepseek-harness-go/plugins/workflow/a.go:3.1,4.1 2 0\n"

const protocolPass = "github.com/gosomea/deepseek-harness-go/protocol/a.go:1.1,2.1 9 1\ngithub.com/gosomea/deepseek-harness-go/protocol/a.go:3.1,4.1 1 0\n"
const protocolFail = "github.com/gosomea/deepseek-harness-go/protocol/a.go:1.1,2.1 8 1\ngithub.com/gosomea/deepseek-harness-go/protocol/a.go:3.1,4.1 2 0\n"
const hostapiPass = "github.com/gosomea/deepseek-harness-go/plugins/hostapi/a.go:1.1,2.1 9 1\ngithub.com/gosomea/deepseek-harness-go/plugins/hostapi/a.go:3.1,4.1 1 0\n"
const hostapiFail = "github.com/gosomea/deepseek-harness-go/plugins/hostapi/a.go:1.1,2.1 8 1\ngithub.com/gosomea/deepseek-harness-go/plugins/hostapi/a.go:3.1,4.1 2 0\n"
const sdkPass = "github.com/gosomea/deepseek-harness-go/plugins/sdk/a.go:1.1,2.1 9 1\ngithub.com/gosomea/deepseek-harness-go/plugins/sdk/a.go:3.1,4.1 1 0\n"
const sdkFail = "github.com/gosomea/deepseek-harness-go/plugins/sdk/a.go:1.1,2.1 8 1\ngithub.com/gosomea/deepseek-harness-go/plugins/sdk/a.go:3.1,4.1 2 0\n"
const mcpPass = "github.com/gosomea/deepseek-harness-go/plugins/mcp/a.go:1.1,2.1 9 1\ngithub.com/gosomea/deepseek-harness-go/plugins/mcp/a.go:3.1,4.1 1 0\n"
const mcpFail = "github.com/gosomea/deepseek-harness-go/plugins/mcp/a.go:1.1,2.1 8 1\ngithub.com/gosomea/deepseek-harness-go/plugins/mcp/a.go:3.1,4.1 2 0\n"
const lspPass = "github.com/gosomea/deepseek-harness-go/plugins/lsp/a.go:1.1,2.1 9 1\ngithub.com/gosomea/deepseek-harness-go/plugins/lsp/a.go:3.1,4.1 1 0\n"
const lspFail = "github.com/gosomea/deepseek-harness-go/plugins/lsp/a.go:1.1,2.1 8 1\ngithub.com/gosomea/deepseek-harness-go/plugins/lsp/a.go:3.1,4.1 2 0\n"
const sshPass = "github.com/gosomea/deepseek-harness-go/plugins/ssh/a.go:1.1,2.1 9 1\ngithub.com/gosomea/deepseek-harness-go/plugins/ssh/a.go:3.1,4.1 1 0\n"
const sshFail = "github.com/gosomea/deepseek-harness-go/plugins/ssh/a.go:1.1,2.1 8 1\ngithub.com/gosomea/deepseek-harness-go/plugins/ssh/a.go:3.1,4.1 2 0\n"
const hooksPass = "github.com/gosomea/deepseek-harness-go/plugins/hooks/a.go:1.1,2.1 9 1\ngithub.com/gosomea/deepseek-harness-go/plugins/hooks/a.go:3.1,4.1 1 0\n"
const hooksFail = "github.com/gosomea/deepseek-harness-go/plugins/hooks/a.go:1.1,2.1 8 1\ngithub.com/gosomea/deepseek-harness-go/plugins/hooks/a.go:3.1,4.1 2 0\n"
const webPass = "github.com/gosomea/deepseek-harness-go/plugins/web/a.go:1.1,2.1 9 1\ngithub.com/gosomea/deepseek-harness-go/plugins/web/a.go:3.1,4.1 1 0\n"
const webFail = "github.com/gosomea/deepseek-harness-go/plugins/web/a.go:1.1,2.1 8 1\ngithub.com/gosomea/deepseek-harness-go/plugins/web/a.go:3.1,4.1 2 0\n"
const clientviewPass = "github.com/gosomea/deepseek-harness-go/plugins/clientview/a.go:1.1,2.1 9 1\ngithub.com/gosomea/deepseek-harness-go/plugins/clientview/a.go:3.1,4.1 1 0\n"
const clientviewFail = "github.com/gosomea/deepseek-harness-go/plugins/clientview/a.go:1.1,2.1 8 1\ngithub.com/gosomea/deepseek-harness-go/plugins/clientview/a.go:3.1,4.1 2 0\n"
const desktophostPass = "github.com/gosomea/deepseek-harness-go/plugins/desktophost/a.go:1.1,2.1 9 1\ngithub.com/gosomea/deepseek-harness-go/plugins/desktophost/a.go:3.1,4.1 1 0\n"
const desktophostFail = "github.com/gosomea/deepseek-harness-go/plugins/desktophost/a.go:1.1,2.1 8 1\ngithub.com/gosomea/deepseek-harness-go/plugins/desktophost/a.go:3.1,4.1 2 0\n"
const conformancePass = "github.com/gosomea/deepseek-harness-go/internal/conformance/a.go:1.1,2.1 9 1\ngithub.com/gosomea/deepseek-harness-go/internal/conformance/a.go:3.1,4.1 1 0\n"
const conformanceFail = "github.com/gosomea/deepseek-harness-go/internal/conformance/a.go:1.1,2.1 8 1\ngithub.com/gosomea/deepseek-harness-go/internal/conformance/a.go:3.1,4.1 2 0\n"
const telemetryPass = "github.com/gosomea/deepseek-harness-go/plugins/telemetry/a.go:1.1,2.1 9 1\ngithub.com/gosomea/deepseek-harness-go/plugins/telemetry/a.go:3.1,4.1 1 0\n"
const telemetryFail = "github.com/gosomea/deepseek-harness-go/plugins/telemetry/a.go:1.1,2.1 8 1\ngithub.com/gosomea/deepseek-harness-go/plugins/telemetry/a.go:3.1,4.1 2 0\n"
