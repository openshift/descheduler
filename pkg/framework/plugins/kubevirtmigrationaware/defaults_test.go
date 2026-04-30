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
)

func TestSetDefaults(t *testing.T) {
	t.Run("zero value gets all defaults", func(t *testing.T) {
		args := &KubevirtMigrationAwareArgs{}
		SetDefaults_KubevirtMigrationAwareArgs(args)
		if args.MigrationCooldown.Duration != defaultMigrationCooldown {
			t.Errorf("MigrationCooldown = %v, want %v", args.MigrationCooldown.Duration, defaultMigrationCooldown)
		}
		if args.MaxMigrationCooldown.Duration != defaultMaxMigrationCooldown {
			t.Errorf("MaxMigrationCooldown = %v, want %v", args.MaxMigrationCooldown.Duration, defaultMaxMigrationCooldown)
		}
		if args.MigrationHistoryWindow.Duration != defaultMigrationHistoryWindow {
			t.Errorf("MigrationHistoryWindow = %v, want %v", args.MigrationHistoryWindow.Duration, defaultMigrationHistoryWindow)
		}
	})

	t.Run("explicit values are preserved", func(t *testing.T) {
		args := &KubevirtMigrationAwareArgs{}
		args.MigrationCooldown.Duration = 10 * time.Minute
		args.MaxMigrationCooldown.Duration = 4 * time.Hour
		args.MigrationHistoryWindow.Duration = 12 * time.Hour
		SetDefaults_KubevirtMigrationAwareArgs(args)
		if args.MigrationCooldown.Duration != 10*time.Minute {
			t.Errorf("MigrationCooldown = %v, want 10m", args.MigrationCooldown.Duration)
		}
		if args.MaxMigrationCooldown.Duration != 4*time.Hour {
			t.Errorf("MaxMigrationCooldown = %v, want 4h", args.MaxMigrationCooldown.Duration)
		}
		if args.MigrationHistoryWindow.Duration != 12*time.Hour {
			t.Errorf("MigrationHistoryWindow = %v, want 12h", args.MigrationHistoryWindow.Duration)
		}
	})
}
