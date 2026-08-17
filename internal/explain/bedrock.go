// Package explain streams a natural-language analysis of diagnostic findings
// from Amazon Bedrock Nova Lite — the only sudiviz feature that uses LLM tokens.
package explain

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"

	"github.com/pydevsg/sudiviz-go/internal/diagnose"
)

const (
	// ModelID is Amazon Nova Lite — same model the Python CLI uses.
	ModelID = "amazon.nova-lite-v1:0"

	generalSystem = `You are an AWS infrastructure expert. You will receive a structured JSON diagnostic report from sudiviz, a tool that analyses live AWS infrastructure. Your job is to:
1. Explain the findings in plain English — what is broken and WHY.
2. Connect dots across findings to identify root causes (e.g. a single misconfiguration causing multiple symptoms).
3. Produce a prioritised action plan, most critical items first.
4. Be concise but thorough. Use bullet points and clear headings.
Do NOT repeat the raw JSON back. Synthesise and explain.`

	questionSystem = `You are an AWS infrastructure expert. You will receive a structured JSON diagnostic report from sudiviz and a specific question from the user. Answer ONLY the user's question directly and concisely. Do not provide a full diagnosis or list all findings — just answer the question. Keep it short: a few sentences or a short bullet list at most. Do NOT repeat the raw JSON back.`
)

// BedrockAPI is the subset of the Bedrock Runtime client used for streaming.
type BedrockAPI interface {
	ConverseStream(ctx context.Context, params *bedrockruntime.ConverseStreamInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseStreamOutput, error)
}

// Request is one explain invocation.
type Request struct {
	Diagnosis *diagnose.Diagnosis
	Question  string
}

// Stream writes Nova Lite's response to w as tokens arrive.
func Stream(ctx context.Context, api BedrockAPI, req Request, w io.Writer) error {
	if req.Diagnosis == nil || len(req.Diagnosis.Fixes) == 0 {
		_, _ = io.WriteString(w, "No issues found — nothing to explain!\n")
		return nil
	}
	findings, err := json.MarshalIndent(req.Diagnosis.ToMap(), "", "  ")
	if err != nil {
		return err
	}

	system := generalSystem
	user := fmt.Sprintf("Here are the diagnostic findings:\n\n```json\n%s\n```", findings)
	if strings.TrimSpace(req.Question) != "" {
		system = questionSystem
		user = fmt.Sprintf("Question: %s\n\nDiagnostic context:\n```json\n%s\n```", req.Question, findings)
	}

	out, err := api.ConverseStream(ctx, &bedrockruntime.ConverseStreamInput{
		ModelId: aws.String(ModelID),
		System: []types.SystemContentBlock{
			&types.SystemContentBlockMemberText{Value: system},
		},
		Messages: []types.Message{{
			Role: types.ConversationRoleUser,
			Content: []types.ContentBlock{
				&types.ContentBlockMemberText{Value: user},
			},
		}},
		InferenceConfig: &types.InferenceConfiguration{
			MaxTokens:   aws.Int32(4096),
			Temperature: aws.Float32(0.3),
		},
	})
	if err != nil {
		return fmt.Errorf("bedrock converse: %w", err)
	}

	stream := out.GetStream()
	defer stream.Close()
	for event := range stream.Events() {
		switch v := event.(type) {
		case *types.ConverseStreamOutputMemberContentBlockDelta:
			if v.Value.Delta == nil {
				continue
			}
			if delta, ok := v.Value.Delta.(*types.ContentBlockDeltaMemberText); ok {
				if _, err := io.WriteString(w, delta.Value); err != nil {
					return err
				}
			}
		}
	}
	if err := stream.Err(); err != nil {
		return fmt.Errorf("bedrock stream: %w", err)
	}
	_, _ = io.WriteString(w, "\n")
	return nil
}
