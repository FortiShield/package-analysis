package worker

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/khulnasoft-lab/package-analysis/internal/analysis"
	"github.com/khulnasoft-lab/package-analysis/internal/log"
	"github.com/khulnasoft-lab/package-analysis/internal/pkgmanager"
	"github.com/khulnasoft-lab/package-analysis/internal/sandbox"
	"github.com/khulnasoft-lab/package-analysis/internal/staticanalysis"
	"github.com/khulnasoft-lab/package-analysis/internal/utils"
	"github.com/khulnasoft-lab/package-analysis/pkg/api/analysisrun"
)

// defaultStaticAnalysisImage is the default Docker image for the static analysis sandbox.
const defaultStaticAnalysisImage = "ghcr.io/khulnasoft-lab/static-analysis"

// staticAnalyzeBinary is the absolute path to the compiled staticanalyze.go binary
// inside the static analysis sandbox (see sandboxes/staticanalysis/Dockerfile).
const staticAnalyzeBinary = "/usr/local/bin/staticanalyze"

// resultsJSONFile is the absolute path to the shared mount inside the static analysis sandbox
// where the output results JSON data should be written.
const resultsJSONFile = "/results.json"

// RunStaticAnalysis performs the given static analysis tasks on package code,
// in a sandboxed environment.
//
// To run all available static analyses, pass staticanalysis.All as tasks.
// Use sbOpts to customise sandbox behaviour.
func RunStaticAnalysis(ctx context.Context, pkg *pkgmanager.Pkg, sbOpts []sandbox.Option, tasks ...staticanalysis.Task) (analysisrun.StaticAnalysisResults, analysis.Status, error) {
	ctx = log.ContextWithAttrs(ctx, slog.String("mode", "static"))

	slog.InfoContext(ctx, "Running static analysis", "tasks", tasks)

	startTime := time.Now()

	analyses := utils.Transform(tasks, func(t staticanalysis.Task) string { return string(t) })

	args := []string{
		"-ecosystem", pkg.EcosystemName(),
		"-package", pkg.Name(),
		"-version", pkg.Version(),
		"-analyses", strings.Join(analyses, ","),
		"-output", resultsJSONFile,
	}

	if pkg.IsLocal() {
		args = append(args, "-local", pkg.LocalPath())
	}

	// create the results JSON file as an empty file, so it can be mounted into the container
	resultsFile, err := os.OpenFile(resultsJSONFile, os.O_RDONLY|os.O_CREATE, 0o644)
	if err != nil {
		return nil, "", fmt.Errorf("could not create results JSON file: %w", err)
	}
	_ = resultsFile.Close()

	// for saving static analysis results inside the sandbox
	sbOpts = append(sbOpts, sandbox.Volume(resultsJSONFile, resultsJSONFile))

	sb := sandbox.New(sbOpts...)
	defer func() {
		if err := sb.Clean(); err != nil {
			slog.ErrorContext(ctx, "Error cleaning up sandbox", "error", err)
		}
	}()

	runResult, err := sb.Run(staticAnalyzeBinary, args...)
	if err != nil {
		return nil, "", fmt.Errorf("sandbox failed (%w)", err)
	}

	resultsJSON, err := os.ReadFile(resultsJSONFile)
	if err != nil {
		return nil, "", fmt.Errorf("could not read results JSON file: %w", err)
	}

	slog.InfoContext(ctx, "Got results", "length", len(resultsJSON))

	status := analysis.StatusForRunResult(runResult)

	totalTime := time.Since(startTime)
	slog.InfoContext(ctx, "Static analysis finished",
		log.LabelAttr("result_status", string(status)),
		"static_analysis_duration", totalTime,
	)

	return resultsJSON, status, nil
}
