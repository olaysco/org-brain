package ai

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
)

type fakeRuntime struct {
	actions    []types.GuardrailAction
	checkError error
	calls      int
	modelCalls int
	stop       types.StopReason
}

func (f *fakeRuntime) ApplyGuardrail(context.Context, *bedrockruntime.ApplyGuardrailInput, ...func(*bedrockruntime.Options)) (*bedrockruntime.ApplyGuardrailOutput, error) {
	if f.checkError != nil {
		return nil, f.checkError
	}
	action := f.actions[f.calls]
	f.calls++
	return &bedrockruntime.ApplyGuardrailOutput{Action: action}, nil
}
func (f *fakeRuntime) Converse(context.Context, *bedrockruntime.ConverseInput, ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
	f.modelCalls++
	return &bedrockruntime.ConverseOutput{StopReason: f.stop, Output: &types.ConverseOutputMemberMessage{Value: types.Message{Content: []types.ContentBlock{&types.ContentBlockMemberText{Value: "answer"}}}}, Usage: &types.TokenUsage{InputTokens: aws.Int32(1)}}, nil
}
func TestGenerateStopsAtGuardrail(t *testing.T) {
	for _, failure := range []bool{false, true} {
		runtime := &fakeRuntime{actions: []types.GuardrailAction{types.GuardrailActionGuardrailIntervened}}
		if failure {
			runtime.checkError = errors.New("unavailable")
		}
		_, err := (Client{Runtime: runtime}).Generate(context.Background(), "instruction", "input", 100)
		if err == nil || runtime.modelCalls != 0 {
			t.Fatal("blocked or failed check reached model")
		}
	}
}
func TestGenerateOutputCheck(t *testing.T) {
	runtime := &fakeRuntime{actions: []types.GuardrailAction{types.GuardrailActionNone, types.GuardrailActionGuardrailIntervened}, stop: types.StopReasonEndTurn}
	if _, err := (Client{Runtime: runtime}).Generate(context.Background(), "instruction", "input", 100); err == nil {
		t.Fatal("blocked output returned")
	}
}
func TestGenerateRejectsTruncation(t *testing.T) {
	runtime := &fakeRuntime{actions: []types.GuardrailAction{types.GuardrailActionNone}, stop: types.StopReasonMaxTokens}
	if _, err := (Client{Runtime: runtime}).Generate(context.Background(), "instruction", "input", 100); err == nil {
		t.Fatal("truncated output accepted")
	}
}
