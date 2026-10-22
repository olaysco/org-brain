package ai

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
)

type Runtime interface {
	ApplyGuardrail(context.Context, *bedrockruntime.ApplyGuardrailInput, ...func(*bedrockruntime.Options)) (*bedrockruntime.ApplyGuardrailOutput, error)
	Converse(context.Context, *bedrockruntime.ConverseInput, ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error)
}

type Client struct {
	Runtime          Runtime
	ModelID          string
	GuardrailID      string
	GuardrailVersion string
}

func (c Client) Check(ctx context.Context, text string, source types.GuardrailContentSource) error {
	result, err := c.Runtime.ApplyGuardrail(ctx, &bedrockruntime.ApplyGuardrailInput{
		GuardrailIdentifier: aws.String(c.GuardrailID), GuardrailVersion: aws.String(c.GuardrailVersion), Source: source,
		Content: []types.GuardrailContentBlock{&types.GuardrailContentBlockMemberText{Value: types.GuardrailTextBlock{Text: aws.String(text)}}},
	})
	if err != nil {
		return fmt.Errorf("check content: %w", err)
	}
	if result == nil || result.Action != types.GuardrailActionNone {
		return fmt.Errorf("content blocked or guardrail action unavailable")
	}
	return nil
}

func (c Client) Generate(ctx context.Context, instruction, input string, maxTokens int32) (string, error) {
	if err := c.Check(ctx, input, types.GuardrailContentSourceInput); err != nil {
		return "", err
	}
	response, err := c.Runtime.Converse(ctx, &bedrockruntime.ConverseInput{
		ModelId:         aws.String(c.ModelID),
		System:          []types.SystemContentBlock{&types.SystemContentBlockMemberText{Value: instruction}},
		Messages:        []types.Message{{Role: types.ConversationRoleUser, Content: []types.ContentBlock{&types.ContentBlockMemberText{Value: input}}}},
		InferenceConfig: &types.InferenceConfiguration{MaxTokens: aws.Int32(maxTokens)},
	})
	if err != nil {
		return "", fmt.Errorf("generate text: %w", err)
	}
	if response == nil {
		return "", fmt.Errorf("model returned no response")
	}
	if response.StopReason != types.StopReasonEndTurn {
		return "", fmt.Errorf("generation stopped: %s", response.StopReason)
	}
	message, ok := response.Output.(*types.ConverseOutputMemberMessage)
	if !ok {
		return "", fmt.Errorf("unexpected model response")
	}
	var output strings.Builder
	for _, block := range message.Value.Content {
		if text, ok := block.(*types.ContentBlockMemberText); ok {
			output.WriteString(text.Value)
		}
	}
	text := strings.TrimSpace(output.String())
	if text == "" {
		return "", fmt.Errorf("model returned an empty response")
	}
	if err := c.Check(ctx, text, types.GuardrailContentSourceOutput); err != nil {
		return "", err
	}
	return text, nil
}
