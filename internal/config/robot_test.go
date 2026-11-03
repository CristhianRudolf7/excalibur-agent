package config

import "testing"

func TestRobotDefaultAndEnvironmentOverride(t *testing.T) {
	if !Default().Robot.Enabled {
		t.Fatal("dev daemon robot should default on")
	}
	cfg, err := Load(LoadOptions{SkipFiles: true, Env: []string{"EXCALIBUR_ROBOT__ENABLED=false"}})
	if err != nil || cfg.Robot.Enabled {
		t.Fatalf("off path: %+v %v", cfg.Robot, err)
	}
}
