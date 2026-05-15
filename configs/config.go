package configs

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config 仿真配置结构体
type Config struct {
	Simulation SimulationConfig `yaml:"simulation"`
	NewPatient PatientConfig    `yaml:"new_patient"`
	RecheckPatient PatientConfig `yaml:"recheck_patient"`
	Preemption PreemptionConfig `yaml:"preemption"`
	Output    OutputConfig      `yaml:"output"`
}

// SimulationConfig 仿真参数
type SimulationConfig struct {
	Duration int `yaml:"duration"`
	Runs     int `yaml:"runs"`
}

// PatientConfig 患者参数
type PatientConfig struct {
	ArrivalRate  float64 `yaml:"arrival_rate"`
	ServiceTime  float64 `yaml:"service_time"`
}

// PreemptionConfig 抢先中断参数
type PreemptionConfig struct {
	ThresholdMinutes int  `yaml:"threshold_minutes"`
	Enabled          bool `yaml:"enabled"`
}

// OutputConfig 输出配置
type OutputConfig struct {
	Verbose   bool   `yaml:"verbose"`
	CSVPath   string `yaml:"csv_path"`
	ChartPath string `yaml:"chart_path"`
}

// Load 加载 YAML 配置文件
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析 YAML 失败: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("配置验证失败: %w", err)
	}

	return &cfg, nil
}

// Validate 验证配置合法性
func (c *Config) Validate() error {
	if c.Simulation.Duration <= 0 {
		return fmt.Errorf("仿真时长必须 > 0，实际: %d", c.Simulation.Duration)
	}
	if c.Simulation.Runs <= 0 {
		return fmt.Errorf("重复次数必须 > 0，实际: %d", c.Simulation.Runs)
	}
	if c.NewPatient.ArrivalRate <= 0 {
		return fmt.Errorf("新诊到达率必须 > 0，实际: %f", c.NewPatient.ArrivalRate)
	}
	if c.NewPatient.ServiceTime <= 0 {
		return fmt.Errorf("新诊服务时长必须 > 0，实际: %f", c.NewPatient.ServiceTime)
	}
	if c.RecheckPatient.ArrivalRate <= 0 {
		return fmt.Errorf("复查到达率必须 > 0，实际: %f", c.RecheckPatient.ArrivalRate)
	}
	if c.RecheckPatient.ServiceTime <= 0 {
		return fmt.Errorf("复查服务时长必须 > 0，实际: %f", c.RecheckPatient.ServiceTime)
	}
	if c.Preemption.ThresholdMinutes < 0 {
		return fmt.Errorf("preemption 阈值必须 >= 0，实际: %d", c.Preemption.ThresholdMinutes)
	}
	return nil
}

// GetDefault 返回默认配置
func GetDefault() *Config {
	return &Config{
		Simulation: SimulationConfig{
			Duration: 480,
			Runs:     100,
		},
		NewPatient: PatientConfig{
			ArrivalRate: 0.1,
			ServiceTime: 10,
		},
		RecheckPatient: PatientConfig{
			ArrivalRate: 0.05,
			ServiceTime: 5,
		},
		Preemption: PreemptionConfig{
			ThresholdMinutes: 3,
			Enabled:          true,
		},
		Output: OutputConfig{
			Verbose:   false,
			CSVPath:   "outputs/results.csv",
			ChartPath: "outputs/sensitivity.png",
		},
	}
}

// ToMap 转换为 map 用于 JSON 序列化
func (c *Config) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"simulation": map[string]interface{}{
			"duration": c.Simulation.Duration,
			"runs":     c.Simulation.Runs,
		},
		"new_patient": map[string]interface{}{
			"arrival_rate":  c.NewPatient.ArrivalRate,
			"service_time":  c.NewPatient.ServiceTime,
			"priority":      0,
		},
		"recheck_patient": map[string]interface{}{
			"arrival_rate": c.RecheckPatient.ArrivalRate,
			"service_time": c.RecheckPatient.ServiceTime,
			"priority":     1,
		},
		"preemption": map[string]interface{}{
			"threshold_minutes": c.Preemption.ThresholdMinutes,
			"enabled":           c.Preemption.Enabled,
		},
		"output": map[string]interface{}{
			"verbose":   c.Output.Verbose,
			"csv_path":   c.Output.CSVPath,
			"chart_path": c.Output.ChartPath,
		},
	}
}