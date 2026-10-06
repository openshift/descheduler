/*
Copyright 2026 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package app

import (
	"io"
	"strings"
	"testing"
)

func TestNewDeschedulerCommand_LoggingAlphaOptions(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name: "LoggingAlphaOptions=false rejects --log-json-split-stream",
			args: []string{
				"--logging-format=json",
				"--log-json-split-stream",
				"--feature-gates=LoggingAlphaOptions=false",
			},
			wantErr: "LoggingAlphaOptions is disabled",
		},
		{
			name: "LoggingAlphaOptions=true accepts --log-json-split-stream",
			args: []string{
				"--logging-format=json",
				"--log-json-split-stream",
				"--feature-gates=LoggingAlphaOptions=true",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := NewDeschedulerCommand(io.Discard)
			if err := cmd.ParseFlags(tc.args); err != nil {
				t.Fatalf("ParseFlags(%v): %v", tc.args, err)
			}
			err := cmd.PreRunE(cmd, cmd.Flags().Args())
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected PreRunE to succeed, got error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected PreRunE to fail with %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected PreRunE error to contain %q, got :%v", tc.wantErr, err)
			}
		})
	}
}
