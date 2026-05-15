package tests

import (
	"os"
	"testing"

	"order-queue-preemption-sim/configs"
)

// Helper function to create a temporary config file
func createTempConfigFile(t *testing.T, content string) string {
	tmpFile, err := os.CreateTemp("", "config_test_*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer tmpFile.Close()

	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}

	return tmpFile.Name()
}

func TestLoadConfig(t *testing.T) {
	// Test loading a valid config file
	content := `
simulation:
  duration: 480
  runs: 10
new_patient:
  arrival_rate: 0.3
  service_time: 15
recheck_patient:
  arrival_rate: 0.1
  service_time: 10
preemption:
  threshold_minutes: 5
  enabled: true
output:
  verbose: false
`
	tmpFile := createTempConfigFile(t, content)
	defer os.Remove(tmpFile)

	cfg, err := configs.Load(tmpFile)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if cfg.NewPatient.ArrivalRate != 0.3 {
		t.Errorf("Expected new_patient arrival_rate 0.3, got %v", cfg.NewPatient.ArrivalRate)
	}
	if cfg.RecheckPatient.ArrivalRate != 0.1 {
		t.Errorf("Expected recheck_patient arrival_rate 0.1, got %v", cfg.RecheckPatient.ArrivalRate)
	}
	if cfg.Preemption.Enabled != true {
		t.Errorf("Expected preemption.enabled true, got %v", cfg.Preemption.Enabled)
	}
}

func TestDefaultConfig(t *testing.T) {
	// Test getting default config
	cfg := configs.GetDefault()

	if cfg.Simulation.Duration <= 0 {
		t.Errorf("Expected positive simulation duration, got %v", cfg.Simulation.Duration)
	}
	if cfg.NewPatient.ArrivalRate <= 0 {
		t.Errorf("Expected positive new_patient arrival_rate, got %v", cfg.NewPatient.ArrivalRate)
	}
}

func TestValidateConfig(t *testing.T) {
	// Test valid config
	cfg := &configs.Config{
		Simulation: configs.SimulationConfig{
			Duration: 480,
			Runs:     10,
		},
		NewPatient: configs.PatientConfig{
			ArrivalRate: 0.3,
			ServiceTime: 15,
		},
		RecheckPatient: configs.PatientConfig{
			ArrivalRate: 0.1,
			ServiceTime: 10,
		},
		Preemption: configs.PreemptionConfig{
			ThresholdMinutes: 5,
			Enabled:          true,
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Errorf("Expected valid config, got error: %v", err)
	}

	// Test invalid config - negative arrival rate
	invalidCfg := &configs.Config{
		Simulation: configs.SimulationConfig{
			Duration: 480,
			Runs:     10,
		},
		NewPatient: configs.PatientConfig{
			ArrivalRate: -0.1, // negative
			ServiceTime: 15,
		},
		RecheckPatient: configs.PatientConfig{
			ArrivalRate: 0.1,
			ServiceTime: 10,
		},
	}

	if err := invalidCfg.Validate(); err == nil {
		t.Error("Expected error for negative arrival rate, got nil")
	}
}

func TestConfigToMap(t *testing.T) {
	// Test converting config to map
	cfg := &configs.Config{
		Simulation: configs.SimulationConfig{
			Duration: 480,
			Runs:     10,
		},
		NewPatient: configs.PatientConfig{
			ArrivalRate: 0.3,
			ServiceTime: 15,
		},
		RecheckPatient: configs.PatientConfig{
			ArrivalRate: 0.1,
			ServiceTime: 10,
		},
		Preemption: configs.PreemptionConfig{
			ThresholdMinutes: 5,
			Enabled:          true,
		},
	}

	m := cfg.ToMap()

	if m == nil {
		t.Error("Expected non-nil map")
	}

	sim, ok := m["simulation"].(map[string]interface{})
	if !ok {
		t.Error("Expected simulation key with map value")
	}
	if sim["duration"].(int) != 480 {
		t.Errorf("Expected duration 480, got %v", sim["duration"])
	}
}

func TestConfigFileNotFound(t *testing.T) {
	// Test loading non-existent file
	_, err := configs.Load("nonexistent_file.yaml")
	if err == nil {
		t.Error("Expected error for non-existent file, got nil")
	}
}