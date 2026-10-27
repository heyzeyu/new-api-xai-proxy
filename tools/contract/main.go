// SPDX-License-Identifier: AGPL-3.0-only
// Run inside a new-api checkout to verify the actual host JavaScript engine.
package main

import (
	"context"
	"encoding/binary"
	"flag"
	"fmt"
	"math"
	"os"
	"slices"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/plugins"
	"github.com/QuantumNous/new-api/relay/channel"
	taskplugin "github.com/QuantumNous/new-api/relay/channel/task/jsplugin"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/billing_setting"
)

func main() {
	sourcePath := flag.String("plugin", "plugin.js", "Plugin source")
	fixturePath := flag.String("fixture", "test/contract-fixture.json", "Portable fixture")
	flag.Parse()
	if err := verify(*sourcePath, *fixturePath); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func verify(sourcePath, fixturePath string) error {
	source, err := os.ReadFile(sourcePath)
	if err != nil {
		return err
	}
	registry := jsplugin.NewRegistry()
	for _, builtin := range jsplugin.DefaultRegistry.Generation().Plugins() {
		code, err := plugins.Source(builtin.Meta.Key)
		if err != nil {
			return err
		}
		if _, err := registry.RegisterFactory(code, jsplugin.Options{}); err != nil {
			return err
		}
	}
	loaded, err := registry.Register(string(source), jsplugin.Options{})
	if err != nil {
		return fmt.Errorf("compile xAI proxy with built-ins: %w", err)
	}
	if loaded.Meta.Key != "xai-proxy" || len(loaded.Meta.ChannelTypes) != 0 || loaded.Meta.Auth.Type != "api_key" {
		return fmt.Errorf("xAI proxy must use its own task-plugin channel")
	}
	for _, model := range loaded.Meta.Models {
		for _, path := range []string{"/v1/videos", "/v1/responses"} {
			keys := make([]string, 0)
			for _, candidate := range registry.Generation().LookupEndpointCandidates("POST", path, model) {
				keys = append(keys, candidate.Plugin.Meta.Key)
			}
			if !slices.Contains(keys, "xai-proxy") || !slices.Contains(keys, "xai") {
				return fmt.Errorf("shared route %s for %s lacks an xAI candidate: %v", path, model, keys)
			}
		}
	}
	for path, want := range map[string]string{
		"/xai-proxy/v1/videos/generations": "xai-proxy",
		"/xai/v1/videos/generations":       "xai",
	} {
		route, found := registry.Generation().LookupDeclaredRoute("POST", path)
		if !found || route.Plugin.Meta.Key != want {
			return fmt.Errorf("incorrect native route ownership for %s", path)
		}
	}
	fixture, err := os.ReadFile(fixturePath)
	if err != nil {
		return err
	}
	report, err := jsplugin.ReplayFixture(context.Background(), string(source), fixture)
	if err != nil {
		return fmt.Errorf("fixture %d/%d: %w", report.Passed, report.Total, err)
	}
	if err = verifyBilling(loaded); err != nil {
		return err
	}
	if err = verifyProtectedProbe(loaded); err != nil {
		return err
	}
	if err = verifyArtifactDescriptor(loaded); err != nil {
		return err
	}
	fmt.Printf("new-api runtime: %s@%s, %d models, %d/%d fixture cases, shared routes, pricing and protected probe passed\n", loaded.Meta.Key, loaded.Meta.Version, len(loaded.Meta.Models), report.Passed, report.Total)
	return nil
}

func verifyArtifactDescriptor(plugin *jsplugin.LoadedPlugin) error {
	data, err := common.Marshal(map[string]any{"status": "done", "video": map[string]any{"url": "/v1/videos/upstream-id/content", "duration": 1}})
	if err != nil {
		return err
	}
	task := &model.Task{TaskID: "task_public", Status: model.TaskStatusSuccess, Data: data, PrivateData: model.TaskPrivateData{UpstreamTaskID: "upstream-id"}}
	adaptor := taskplugin.New(plugin)
	adaptor.Init(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://proxy.example", ApiKey: "fixture-key", ChannelType: 61}})
	content, err := adaptor.BuildContentRequest(context.Background(), task, "video", channel.TaskArtifactClientRequest{Method: "GET"})
	if err != nil {
		return err
	}
	if content == nil || content.URL != "https://proxy.example/v1/videos/upstream-id/content" || content.Credentialless || content.Headers["Authorization"] != "Bearer fixture-key" {
		return fmt.Errorf("host did not construct the credentialed content request")
	}
	return nil
}

func verifyBilling(plugin *jsplugin.LoadedPlugin) error {
	const basic = `u("resolution") == "480p" ? tier("480p", u("input_images") * 0.002 + u("input_video_seconds") * 0.01 + u("seconds") * 0.05) : tier("720p", u("input_images") * 0.002 + u("input_video_seconds") * 0.01 + u("seconds") * 0.07)`
	const v15 = `u("resolution") == "480p" ? tier("480p", u("input_images") * 0.01 + u("seconds") * 0.08) : u("resolution") == "720p" ? tier("720p", u("input_images") * 0.01 + u("seconds") * 0.14) : tier("1080p", u("input_images") * 0.01 + u("seconds") * 0.25)`
	for _, tc := range []struct {
		model, expr string
		facts       map[string]any
		want        float64
	}{
		{"grok-imagine-video", basic, map[string]any{"resolution": "480p", "seconds": 1.0, "input_images": 0.0, "input_video_seconds": 0.0, "moderated": false}, 0.05},
		{"grok-imagine-video", basic, map[string]any{"resolution": "480p", "seconds": 8.0, "input_images": 0.0, "input_video_seconds": 8.0, "moderated": false}, 0.48},
		{"grok-imagine-video", basic, map[string]any{"resolution": "720p", "seconds": 8.0, "input_images": 1.0, "input_video_seconds": 8.0, "moderated": false}, 0.642},
		{"grok-imagine-video-1.5", v15, map[string]any{"resolution": "480p", "seconds": 1.0, "input_images": 0.0, "moderated": false}, 0.08},
		{"grok-imagine-video-1.5", v15, map[string]any{"resolution": "1080p", "seconds": 2.0, "input_images": 1.0, "moderated": false}, 0.51},
	} {
		schema, _ := plugin.Meta.UsageForModel(tc.model)
		if err := billing_setting.SmokeTestTaskExpr(tc.expr, schema); err != nil {
			return fmt.Errorf("pricing schema for %s: %w", tc.model, err)
		}
		got, _, err := billingexpr.RunExprWithRequest(tc.expr, billingexpr.TokenParams{}, billingexpr.RequestInput{Usage: tc.facts})
		if err != nil || math.Abs(got-tc.want) > 1e-9 {
			return fmt.Errorf("pricing for %s: got %v, want %v, err %v", tc.model, got, tc.want, err)
		}
	}
	return nil
}

func verifyProtectedProbe(plugin *jsplugin.LoadedPlugin) error {
	fields := make([]byte, 4+72+8)
	binary.BigEndian.PutUint16(fields[4+72:], 854)
	binary.BigEndian.PutUint16(fields[4+72+4:], 480)
	box := func(kind string, body []byte) []byte {
		header := make([]byte, 8)
		binary.BigEndian.PutUint32(header, uint32(8+len(body)))
		copy(header[4:], kind)
		return append(header, body...)
	}
	file := append(box("ftyp", []byte("isom0000isom")), box("moov", box("trak", box("tkhd", fields)))...)
	requests := 0
	fetcher := func(_ context.Context, request jsplugin.FetchRequest) (jsplugin.FetchResponse, error) {
		requests++
		if request.URL != "https://proxy.example/v1/videos/upstream-id/content" || request.Headers["Authorization"] != "Bearer fixture-key" || request.Headers["Range"] != "bytes=0-65535" {
			return jsplugin.FetchResponse{}, fmt.Errorf("unexpected probe target or credential")
		}
		return jsplugin.FetchResponse{Status: 200, Headers: map[string]string{}, Body: file}, nil
	}
	ctx := jsplugin.WithFetcher(context.Background(), fetcher)
	value, err := plugin.Engine.Call(ctx, "parseTaskResult",
		map[string]any{"action": "video_edit", "taskId": "upstream-id", "baseUrl": "https://proxy.example", "apiKey": "fixture-key"},
		map[string]any{"status": "done", "video": map[string]any{"url": "/v1/videos/upstream-id/content", "duration": 8}},
		map[string]any{"status": 200},
	)
	if err != nil {
		return err
	}
	encoded, err := common.Marshal(value)
	if err != nil {
		return err
	}
	var result struct {
		Status string `json:"status"`
		State  struct {
			Resolution string `json:"output_resolution"`
		} `json:"state"`
	}
	if err = common.Unmarshal(encoded, &result); err != nil {
		return err
	}
	if requests != 1 || result.Status != "SUCCESS" || result.State.Resolution != "480p" {
		return fmt.Errorf("protected video probe did not resolve 480p: %s, requests %d", encoded, requests)
	}
	return nil
}
