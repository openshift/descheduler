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

package kubevirtmigrationaware

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestValidateArgs(t *testing.T) {
	cases := []struct {
		description string
		args        *KubevirtMigrationAwareArgs
		wantErr     bool
	}{
		{
			description: "zero cooldown is valid (disables the cooldown gate)",
			args:        &KubevirtMigrationAwareArgs{},
			wantErr:     false,
		},
		{
			description: "positive cooldown is valid",
			args: &KubevirtMigrationAwareArgs{
				MigrationCooldown: metav1.Duration{Duration: 5 * time.Minute},
			},
			wantErr: false,
		},
		{
			description: "negative cooldown is invalid",
			args: &KubevirtMigrationAwareArgs{
				MigrationCooldown: metav1.Duration{Duration: -1 * time.Second},
			},
			wantErr: true,
		},
		{
			description: "negative maxMigrationCooldown is invalid",
			args: &KubevirtMigrationAwareArgs{
				MaxMigrationCooldown: metav1.Duration{Duration: -1 * time.Second},
			},
			wantErr: true,
		},
		{
			description: "maxMigrationCooldown below migrationCooldown is invalid",
			args: &KubevirtMigrationAwareArgs{
				MigrationCooldown:    metav1.Duration{Duration: 15 * time.Minute},
				MaxMigrationCooldown: metav1.Duration{Duration: 10 * time.Minute},
			},
			wantErr: true,
		},
		{
			description: "maxMigrationCooldown equal to migrationCooldown is valid",
			args: &KubevirtMigrationAwareArgs{
				MigrationCooldown:    metav1.Duration{Duration: 15 * time.Minute},
				MaxMigrationCooldown: metav1.Duration{Duration: 15 * time.Minute},
			},
			wantErr: false,
		},
		{
			description: "maxMigrationCooldown greater than migrationCooldown is valid",
			args: &KubevirtMigrationAwareArgs{
				MigrationCooldown:    metav1.Duration{Duration: 15 * time.Minute},
				MaxMigrationCooldown: metav1.Duration{Duration: 1 * time.Hour},
			},
			wantErr: false,
		},
		{
			description: "zero maxMigrationCooldown (disabled) is always valid",
			args: &KubevirtMigrationAwareArgs{
				MigrationCooldown:    metav1.Duration{Duration: 15 * time.Minute},
				MaxMigrationCooldown: metav1.Duration{Duration: 0},
			},
			wantErr: false,
		},
		{
			description: "positive migrationHistoryWindow is valid",
			args: &KubevirtMigrationAwareArgs{
				MigrationHistoryWindow: metav1.Duration{Duration: 24 * time.Hour},
			},
			wantErr: false,
		},
		{
			description: "negative migrationHistoryWindow is invalid",
			args: &KubevirtMigrationAwareArgs{
				MigrationHistoryWindow: metav1.Duration{Duration: -1 * time.Hour},
			},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			err := ValidateKubevirtMigrationAwareArgs(tc.args)
			if (err != nil) != tc.wantErr {
				t.Errorf("ValidateKubevirtMigrationAwareArgs() error = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}
