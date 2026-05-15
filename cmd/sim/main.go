package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"order-queue-preemption-sim/configs"
	"order-queue-preemption-sim/sqlite"
)

var version = "0.1.0"

// initLog initializes the log file for CLI command execution
func initLog() {
	// Ensure outputs/logs directory exists
	logsDir := "outputs/logs"
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "警告: 无法创建日志目录: %v\n", err)
		return
	}

	logFile := logsDir + "/sim.log"
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "警告: 无法打开日志文件: %v\n", err)
		return
	}

	// MultiWriter: stdout + log file
	log.SetOutput(f)
	log.SetFlags(log.LstdFlags | log.Lshortfile)
}

// initDB initializes the SQLite database
func initDB() {
	dbPath := "outputs/sim_results.db"
	if err := sqlite.InitDB(dbPath); err != nil {
		fmt.Fprintf(os.Stderr, "警告: 无法初始化数据库: %v\n", err)
		return
	}
	fmt.Println("数据库已初始化: " + dbPath)
}

// getPythonCommand returns the appropriate python command based on OS
func getPythonCommand() string {
	if runtime.GOOS == "windows" {
		return "python"
	}
	return "python3"
}

// buildSimParams builds simulation parameters from config
func buildSimParams(cfg *configs.Config, preemptionEnabled bool, threshold int) map[string]interface{} {
	return map[string]interface{}{
		"new_patient_arrival_rate": cfg.NewPatient.ArrivalRate,
		"recheck_arrival_rate":     cfg.RecheckPatient.ArrivalRate,
		"new_patient_service_time": cfg.NewPatient.ServiceTime,
		"recheck_service_time":     cfg.RecheckPatient.ServiceTime,
		"simulation_time":          cfg.Simulation.Duration,
		"seed":                     42,
		"preemption_enabled":       preemptionEnabled,
		"preemption_threshold":     float64(threshold),
	}
}

// callPythonEngine runs the Python simulation engine
func callPythonEngine(params map[string]interface{}) (map[string]interface{}, error) {
	// Find project root (where sim/engine.py exists)
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("获取可执行文件路径失败: %w", err)
	}
	execDir := strings.TrimSuffix(exe, "\\bin\\sim.exe")
	enginePath := execDir + "\\sim\\engine.py"

	// Check if engine.py exists
	if _, err := os.Stat(enginePath); os.IsNotExist(err) {
		// Try relative path for development
		enginePath = "sim/engine.py"
		if _, err := os.Stat(enginePath); os.IsNotExist(err) {
			return nil, fmt.Errorf("找不到 sim/engine.py，请确保在项目目录运行")
		}
	}

	// Validate params before calling Python
	if params == nil {
		return nil, fmt.Errorf("参数为空")
	}

	// Serialize params to JSON
	jsonData, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("序列化参数失败: %w", err)
	}

	// Find Python interpreter
	pythonCmd := getPythonCommand()
	cmd := exec.Command(pythonCmd, []string{enginePath}...)
	cmd.Stdin = strings.NewReader(string(jsonData))

	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderrMsg := string(exitErr.Stderr)
			if stderrMsg != "" {
				return nil, fmt.Errorf("Python 执行失败: %s", stderrMsg)
			}
			return nil, fmt.Errorf("Python 执行失败 (exit code: %d)", exitErr.ExitCode())
		}
		return nil, fmt.Errorf("调用 Python 失败: %w", err)
	}

	// Parse JSON output
	var result map[string]interface{}
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, fmt.Errorf("解析输出 JSON 失败: %w", err)
	}

	// Check if Python returned an error
	if errMsg, ok := result["error"].(string); ok {
		return nil, fmt.Errorf("Python 仿真错误: %s", errMsg)
	}

	return result, nil
}

// findPythonPath finds the Python interpreter path
func findPythonPath() string {
	return getPythonCommand()
}

// findScriptPath finds a Python script path
func findScriptPath(scriptName string) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("获取可执行文件路径失败: %w", err)
	}
	execDir := strings.TrimSuffix(exe, "\\bin\\sim.exe")
	scriptPath := execDir + "\\sim\\" + scriptName

	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		// Try relative path for development
		scriptPath = "sim/" + scriptName
		if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
			return "", fmt.Errorf("找不到 sim/%s", scriptName)
		}
	}
	return scriptPath, nil
}

// callPythonRunnerConcurrently calls the Python runner with Go-level concurrency
func callPythonRunnerConcurrently(jsonInput string) (map[string]interface{}, error) {
	// Parse input to get parameters
	var input map[string]interface{}
	if err := json.Unmarshal([]byte(jsonInput), &input); err != nil {
		return nil, fmt.Errorf("解析输入参数失败: %w", err)
	}

	baseParams := input["base_params"].(map[string]interface{})
	scanVariable := input["scan_variable"].(string)
	scanValues := input["scan_values"].([]interface{})
	runsPerValue := int(input["runs_per_value"].(float64))
	outputCSV := input["output_csv"].(string)

	// Get scan values as floats
	scanValuesFloat := make([]float64, len(scanValues))
	for i, v := range scanValues {
		scanValuesFloat[i] = v.(float64)
	}

	// Run simulations concurrently using goroutines
	type simResult struct {
		params map[string]interface{}
		stats  map[string]interface{}
		err    error
	}

	// Create a channel to receive results
	resultChan := make(chan simResult, len(scanValuesFloat)*runsPerValue)

	// Launch goroutines for each parameter combination
	var wg sync.WaitGroup
	for _, value := range scanValuesFloat {
		for run := 0; run < runsPerValue; run++ {
			wg.Add(1)
			go func(scanValue float64, runIdx int) {
				defer wg.Done()

				params := make(map[string]interface{})
				for k, v := range baseParams {
					params[k] = v
				}
				params[scanVariable] = scanValue
				params["seed"] = 42 + runIdx

				result, err := callPythonEngine(params)
				if err != nil {
					resultChan <- simResult{err: err}
					return
				}

				resultChan <- simResult{
					params: result["parameters"].(map[string]interface{}),
					stats:  result,
				}
			}(value, run)
		}
	}

	// Close channel when all goroutines complete
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect results
	results := []simResult{}
	for r := range resultChan {
		results = append(results, r)
	}

	// Aggregate results
	grouped := make(map[float64][]map[string]interface{})
	for _, r := range results {
		if r.err != nil {
			continue
		}
		value := r.params[scanVariable].(float64)
		grouped[value] = append(grouped[value], r.stats)
	}

	// Calculate aggregated statistics
	aggregated := make([]map[string]interface{}, 0)
	for _, value := range scanValuesFloat {
		runs := grouped[value]
		if len(runs) == 0 {
			continue
		}

		avgWait := sumFloat(runs, "avg_wait_time") / float64(len(runs))
		avgQueue := sumFloat(runs, "avg_queue_length") / float64(len(runs))
		avgUtil := sumFloat(runs, "server_utilization") / float64(len(runs))
		avgPreempt := sumFloat(runs, "preemption_count") / float64(len(runs))
		newAvgWait := sumFloat(runs, "new_patient_avg_wait") / float64(len(runs))
		recheckAvgWait := sumFloat(runs, "recheck_patient_avg_wait") / float64(len(runs))

		aggregated = append(aggregated, map[string]interface{}{
			scanVariable:              value,
			"avg_wait_time":            avgWait,
			"avg_queue_length":        avgQueue,
			"server_utilization":       avgUtil,
			"preemption_count":        avgPreempt,
			"new_patient_avg_wait":    newAvgWait,
			"recheck_patient_avg_wait": recheckAvgWait,
			"total_patients":          sumFloat(runs, "total_patients") / float64(len(runs)),
			"max_wait_time":           maxFloat(runs, "max_wait_time"),
			"max_queue_length":        maxFloat(runs, "max_queue_length"),
			"runs":                    len(runs),
		})
	}

	// Write CSV
	writeAggregatedCSV(aggregated, scanVariable, outputCSV)

	successCount := 0
	for _, r := range results {
		if r.err == nil {
			successCount++
		}
	}

	return map[string]interface{}{
		"scan_variable":       scanVariable,
		"scan_values":         scanValuesFloat,
		"runs_per_value":      runsPerValue,
		"total_runs":          len(scanValuesFloat) * runsPerValue,
		"successful_runs":     successCount,
		"aggregated_results":  aggregated,
		"csv_output":          outputCSV,
	}, nil
}

// sumFloat sums a float field from results, handling int/float mixed types
func sumFloat(results []map[string]interface{}, field string) float64 {
	sum := 0.0
	for _, r := range results {
		val := r[field]
		switch v := val.(type) {
		case float64:
			sum += v
		case int:
			sum += float64(v)
		case int64:
			sum += float64(v)
		}
	}
	return sum
}

// maxFloat finds max value of a float field, handling int/float mixed types
func maxFloat(results []map[string]interface{}, field string) float64 {
	val := results[0][field]
	var max float64
	switch v := val.(type) {
	case float64:
		max = v
	case int:
		max = float64(v)
	case int64:
		max = float64(v)
	}
	for _, r := range results {
		val := r[field]
		switch v := val.(type) {
		case float64:
			if v > max {
				max = v
			}
		case int:
			if float64(v) > max {
				max = float64(v)
			}
		case int64:
			if float64(v) > max {
				max = float64(v)
			}
		}
	}
	return max
}

// writeAggregatedCSV writes aggregated results to CSV
func writeAggregatedCSV(aggregated []map[string]interface{}, scanVariable, csvPath string) {
	os.MkdirAll("outputs", 0755)

	file, err := os.Create(csvPath)
	if err != nil {
		return
	}
	defer file.Close()

	fmt.Fprintln(file, scanVariable+",avg_wait_time,avg_queue_length,server_utilization,preemption_count,new_patient_avg_wait,recheck_patient_avg_wait,total_patients,max_wait_time,max_queue_length,runs")
	for _, row := range aggregated {
		// Helper to get float64 from various numeric types
		toFloat := func(v interface{}) float64 {
			switch val := v.(type) {
			case float64:
				return val
			case int:
				return float64(val)
			case int64:
				return float64(val)
			default:
				return 0
			}
		}
		fmt.Fprintf(file, "%.0f,%.2f,%.2f,%.4f,%.1f,%.2f,%.2f,%.0f,%.2f,%.0f,%d\n",
			toFloat(row[scanVariable]),
			toFloat(row["avg_wait_time"]),
			toFloat(row["avg_queue_length"]),
			toFloat(row["server_utilization"]),
			toFloat(row["preemption_count"]),
			toFloat(row["new_patient_avg_wait"]),
			toFloat(row["recheck_patient_avg_wait"]),
			toFloat(row["total_patients"]),
			toFloat(row["max_wait_time"]),
			toFloat(row["max_queue_length"]),
			int(toFloat(row["runs"])))
	}
}

// saveToSQLite saves a simulation result to SQLite database
func saveToSQLite(result map[string]interface{}) error {
	if sqlite.DB == nil {
		return fmt.Errorf("数据库未初始化")
	}

	params, ok := result["parameters"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("无法获取参数")
	}

	r := &sqlite.SimulationResult{
		Timestamp:             time.Now(),
		NewPatientRate:        toFloat(params["new_patient_arrival_rate"]),
		RecheckPatientRate:    toFloat(params["recheck_arrival_rate"]),
		NewPatientService:     toFloat(params["new_patient_service_time"]),
		RecheckService:         toFloat(params["recheck_service_time"]),
		SimulationDuration:     int(toFloat(params["simulation_time"])),
		PreemptionEnabled:      toBool(params["preemption_enabled"]),
		PreemptionThreshold:    toFloat(params["preemption_threshold"]),
		TotalPatients:          int(toFloat(result["total_patients"])),
		NewPatients:            int(toFloat(result["new_patients"])),
		RecheckPatients:        int(toFloat(result["recheck_patients"])),
		AvgWaitTime:            toFloat(result["avg_wait_time"]),
		MaxWaitTime:            toFloat(result["max_wait_time"]),
		AvgQueueLength:         toFloat(result["avg_queue_length"]),
		MaxQueueLength:         int(toFloat(result["max_queue_length"])),
		ServerUtilization:      toFloat(result["server_utilization"]),
		NewPatientAvgWait:      toFloat(result["new_patient_avg_wait"]),
		RecheckPatientAvgWait:  toFloat(result["recheck_patient_avg_wait"]),
		PreemptionCount:        int(toFloat(result["preemption_count"])),
		NewPatientPreempted:    int(toFloat(result["new_patient_preempted"])),
		RecheckPatientPreempted: int(toFloat(result["recheck_patient_preempted"])),
		Seed:                   int64(toFloat(params["seed"])),
	}

	_, err := sqlite.SaveResult(r)
	return err
}

// toFloat safely converts interface{} to float64
func toFloat(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case int:
		return float64(val)
	case int64:
		return float64(val)
	default:
		return 0
	}
}

// toBool safely converts interface{} to bool
func toBool(v interface{}) bool {
	switch val := v.(type) {
	case bool:
		return val
	case float64:
		return val != 0
	case int:
		return val != 0
	default:
		return false
	}
}

// generateChart generates chart using visualize.py
func generateChart(csvPath, chartPath, scanVariable, title string) {
	visualizeInput := map[string]interface{}{
		"csv_path":      csvPath,
		"output_path":   chartPath,
		"scan_variable": scanVariable,
		"title":         title,
	}

	vizJSONData, err := json.Marshal(visualizeInput)
	if err != nil {
		fmt.Fprintf(os.Stderr, "警告: 序列化可视化参数失败: %v\n", err)
		return
	}

	vizPath, err := findScriptPath("visualize.py")
	if err != nil {
		fmt.Fprintf(os.Stderr, "警告: 找不到 visualize.py: %v\n", err)
		return
	}

	fmt.Println("正在生成图表...")
	pythonCmd := findPythonPath()
	cmdViz := exec.Command(pythonCmd, vizPath)
	cmdViz.Stdin = strings.NewReader(string(vizJSONData))

	vizOutput, err := cmdViz.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			fmt.Fprintf(os.Stderr, "警告: 图表生成失败: %s\n", string(exitErr.Stderr))
		}
		return
	}

	fmt.Printf("图表生成完成: %s\n", chartPath)
	_ = string(vizOutput)
}

// displayAggregatedResults displays aggregated results in table format
func displayAggregatedResults(result map[string]interface{}, scanVariable string) {
	aggregated, ok := result["aggregated_results"].([]interface{})
	if !ok || len(aggregated) == 0 {
		return
	}

	// Helper function for safe interface to float64 conversion
	toFloat := func(v interface{}) float64 {
		switch val := v.(type) {
		case float64:
			return val
		case int:
			return float64(val)
		case int64:
			return float64(val)
		default:
			return 0
		}
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "\n=== 敏感性分析汇总 ===")
	fmt.Fprintf(w, "%s\t平均等待\t新诊等待\t复查等待\t医生利用率\t中断次数\n", scanVariable)
	fmt.Fprintln(w, "----\t----\t----\t----\t----\t----")
	for _, item := range aggregated {
		row := item.(map[string]interface{})
		fmt.Fprintf(w, "%.0f\t%.2f\t%.2f\t%.2f\t%.1f%%\t%.0f\n",
			toFloat(row[scanVariable]),
			toFloat(row["avg_wait_time"]),
			toFloat(row["new_patient_avg_wait"]),
			toFloat(row["recheck_patient_avg_wait"]),
			toFloat(row["server_utilization"])*100,
			toFloat(row["preemption_count"]))
	}
	w.Flush()

	// Find optimal threshold
	bestThreshold := 0.0
	bestWait := toFloat(aggregated[0].(map[string]interface{})["avg_wait_time"])
	baselineWait := bestWait

	for _, item := range aggregated {
		row := item.(map[string]interface{})
		waitTime := toFloat(row["avg_wait_time"])
		if waitTime < bestWait {
			bestWait = waitTime
			bestThreshold = toFloat(row[scanVariable])
		}
	}

	// Calculate improvement
	improvement := 0.0
	if baselineWait > 0 {
		improvement = (baselineWait - bestWait) / baselineWait * 100
	}

	// Output recommendation
	fmt.Println("\n=== 最优参数推荐 ===")
	if bestThreshold == 0 {
		fmt.Println("建议: 禁用 preemption（复查不打断新诊）")
		fmt.Printf("原因: 所有阈值设置下的平均等待时间均无改善\n")
	} else if bestThreshold >= 15 {
		fmt.Println("建议: 设置 preemption 阈值为 15 分钟或更高")
		fmt.Printf("最优阈值: %.0f 分钟（平均等待时间: %.2f 分钟）\n", bestThreshold, bestWait)
		fmt.Printf("相对阈值=0 时改善: %.1f%%\n", improvement)
		fmt.Println("提示: 高阈值意味着新诊患者不会被频繁中断")
	} else {
		fmt.Printf("建议: 设置 preemption 阈值为 %.0f 分钟\n", bestThreshold)
		fmt.Printf("最优阈值: %.0f 分钟（平均等待时间: %.2f 分钟）\n", bestThreshold, bestWait)
		fmt.Printf("相对阈值=0 时改善: %.1f%%\n", improvement)
		if bestThreshold <= 3 {
			fmt.Println("提示: 低阈值有利于复查患者，但可能增加新诊被中断次数")
		} else {
			fmt.Println("提示: 中等阈值在复查等待和新诊中断之间取得平衡")
		}
	}
}

// formatResults prints simulation results in a formatted table
func formatResults(results map[string]interface{}) {
	// Use proper table formatting that works regardless of terminal width
	fmt.Println()
	fmt.Println("=== 仿真结果 ===")
	fmt.Println()
	printSection("仿真参数", func() {
		params, ok := results["parameters"].(map[string]interface{})
		if ok {
			printRow("新诊到达率", fmt.Sprintf("%.2f 人/分钟", params["new_patient_arrival_rate"]))
			printRow("复查到达率", fmt.Sprintf("%.2f 人/分钟", params["recheck_arrival_rate"]))
			printRow("新诊服务时长", fmt.Sprintf("%.1f 分钟", params["new_patient_service_time"]))
			printRow("复查服务时长", fmt.Sprintf("%.1f 分钟", params["recheck_service_time"]))
			printRow("仿真时长", fmt.Sprintf("%d 分钟", int(params["simulation_time"].(float64))))
			printRow("Preemption", fmt.Sprintf("%v", params["preemption_enabled"]))
			if params["preemption_enabled"].(bool) {
				printRow("Preemption阈值", fmt.Sprintf("%.1f 分钟", params["preemption_threshold"]))
			}
		}
	})

	printSection("患者统计", func() {
		printRow("总患者数", fmt.Sprintf("%d", int(results["total_patients"].(float64))))
		printRow("新诊患者", fmt.Sprintf("%d", int(results["new_patients"].(float64))))
		printRow("复查患者", fmt.Sprintf("%d", int(results["recheck_patients"].(float64))))
	})

	printSection("等待时间", func() {
		printRow("平均等待时间", fmt.Sprintf("%.2f 分钟", results["avg_wait_time"]))
		printRow("最大等待时间", fmt.Sprintf("%.2f 分钟", results["max_wait_time"]))
		printRow("新诊平均等待", fmt.Sprintf("%.2f 分钟", results["new_patient_avg_wait"]))
		printRow("复查平均等待", fmt.Sprintf("%.2f 分钟", results["recheck_patient_avg_wait"]))
	})

	printSection("队列统计", func() {
		printRow("平均队列长度", fmt.Sprintf("%.2f", results["avg_queue_length"]))
		printRow("最大队列长度", fmt.Sprintf("%d", int(results["max_queue_length"].(float64))))
	})

	printSection("医生利用率", func() {
		printRow("医生利用率", fmt.Sprintf("%.1f%%", results["server_utilization"].(float64)*100))
	})

	printSection("Preemption 中断", func() {
		printRow("中断次数", fmt.Sprintf("%d", int(results["preemption_count"].(float64))))
		printRow("被中断新诊", fmt.Sprintf("%d", int(results["new_patient_preempted"].(float64))))
		printRow("被中断复查", fmt.Sprintf("%d", int(results["recheck_patient_preempted"].(float64))))
	})
}

// tableFormatter provides consistent table formatting
type tableFormatter struct {
	rows [][]string
}

func newTableFormatter() *tableFormatter {
	return &tableFormatter{rows: make([][]string, 0)}
}

func (t *tableFormatter) addRow(cells ...string) {
	t.rows = append(t.rows, cells)
}

func (t *tableFormatter) addHeader(cells ...string) {
	t.rows = append(t.rows, cells)
}

func (t *tableFormatter) print() {
	if len(t.rows) == 0 {
		return
	}

	// Calculate column widths
	colCount := len(t.rows[0])
	colWidths := make([]int, colCount)
	for _, row := range t.rows {
		for i, cell := range row {
			if len(cell) > colWidths[i] {
				colWidths[i] = len(cell)
			}
		}
	}

	// Print rows with alignment
	for rowIdx, row := range t.rows {
		// Print separator line for header
		if rowIdx == 1 {
			line := ""
			for i, width := range colWidths {
				if i > 0 {
					line += "  "
				}
				for j := 0; j < width; j++ {
					line += "-"
				}
			}
			fmt.Println(line)
		}

		// Print row cells
		for i, cell := range row {
			if i > 0 {
				fmt.Print("  ")
			}
			fmt.Printf("%-*s", colWidths[i], cell)
		}
		fmt.Println()
	}
}

// printRow prints a single row in table format
func printRow(label, value string) {
	fmt.Printf("  %-20s %s\n", label, value)
}

// printSection prints a section header and content
func printSection(title string, content func()) {
	fmt.Printf("\n%s\n", title)
	fmt.Println(strings.Repeat("-", 40))
	content()
}

// runCmd represents the run command
var runCmd = &cobra.Command{
	Use:   "run",
	Short: "运行单次仿真",
	Long:  `从默认配置或指定配置文件读取参数，运行 SimPy 仿真，输出结果表格`,
	Run:   runSimulation,
}

func init() {
	rootCmd.AddCommand(runCmd)
	runCmd.Flags().StringP("config", "c", "", "配置文件路径（默认为 configs/default.yaml）")
	runCmd.Flags().BoolP("json", "j", false, "输出原始 JSON 格式")
}

func runSimulation(cmd *cobra.Command, args []string) {
	initLog()
	log.Printf("[run] 启动仿真命令")

	configPath, _ := cmd.Flags().GetString("config")
	outputJSON, _ := cmd.Flags().GetBool("json")

	// Load config
	var cfg *configs.Config
	var err error

	if configPath != "" {
		log.Printf("[run] 使用配置文件: %s", configPath)
		cfg, err = configs.Load(configPath)
		if err != nil {
			log.Printf("[run] 错误: 加载配置失败: %v", err)
			fmt.Fprintf(os.Stderr, "错误: 加载配置失败: %v\n", err)
			os.Exit(1)
		}
	} else {
		// Use default config
		log.Printf("[run] 使用默认配置")
		cfg = configs.GetDefault()
	}

	// Build simulation params
	params := buildSimParams(cfg, cfg.Preemption.Enabled, cfg.Preemption.ThresholdMinutes)
	log.Printf("[run] 仿真参数: new_rate=%.2f, recheck_rate=%.2f, preemption=%v, threshold=%d",
		cfg.NewPatient.ArrivalRate, cfg.RecheckPatient.ArrivalRate,
		cfg.Preemption.Enabled, cfg.Preemption.ThresholdMinutes)

	// Run simulation
	fmt.Println("正在运行仿真...")
	log.Printf("[run] 开始执行 Python 仿真引擎")
	results, err := callPythonEngine(params)
	if err != nil {
		log.Printf("[run] 错误: 仿真执行失败: %v", err)
		fmt.Fprintf(os.Stderr, "错误: 仿真执行失败: %v\n", err)
		os.Exit(1)
	}

	log.Printf("[run] 仿真完成: total_patients=%d, avg_wait=%.2f",
		int(results["total_patients"].(float64)), results["avg_wait_time"])

	// Save to SQLite
	if err := saveToSQLite(results); err != nil {
		log.Printf("[run] 警告: 保存到数据库失败: %v", err)
	} else {
		fmt.Println("结果已保存到数据库")
	}

	// Output results
	if outputJSON {
		jsonBytes, _ := json.MarshalIndent(results, "", "  ")
		fmt.Println(string(jsonBytes))
	} else {
		formatResults(results)
	}
	log.Printf("[run] 命令执行完成")
}

// executeCmd handles the execute subcommand
var executeCmd = &cobra.Command{
	Use:   "execute",
	Short: "执行自定义参数仿真",
	Long: `通过命令行参数指定仿真参数，直接运行仿真（不读取配置文件）。

无需 YAML 配置文件，通过命令行直接指定所有参数。适合快速测试或脚本调用。

参数说明：
  --new-rate       新诊到达率 (人/分钟)，默认 0.3
  --recheck-rate   复查到达率 (人/分钟)，默认 0.1
  --new-service    新诊服务时长 (分钟)，默认 10.0
  --recheck-service 复查服务时长 (分钟)，默认 5.0
  --duration       仿真时长 (分钟)，默认 480
  --preemption     是否启用 preemption (true/false)，默认 true
  --threshold      preemption 阈值 (分钟)，默认 3.0

使用示例：
  # 使用默认参数运行
  sim execute

  # 自定义参数
  sim execute --new-rate 0.5 --recheck-rate 0.2 --duration 120

  # 禁用 preemption
  sim execute --preemption=false

  # 输出 JSON 格式便于程序解析
  sim execute -j --new-rate 0.3 --recheck-rate 0.1`,
	Run:   executeSimulation,
}

func init() {
	rootCmd.AddCommand(executeCmd)
	executeCmd.Flags().Float64("new-rate", 0.3, "新诊到达率 (人/分钟)")
	executeCmd.Flags().Float64("recheck-rate", 0.1, "复查到达率 (人/分钟)")
	executeCmd.Flags().Float64("new-service", 10.0, "新诊服务时长 (分钟)")
	executeCmd.Flags().Float64("recheck-service", 5.0, "复查服务时长 (分钟)")
	executeCmd.Flags().Int("duration", 480, "仿真时长 (分钟)")
	executeCmd.Flags().Bool("preemption", true, "启用 preemption")
	executeCmd.Flags().Float64("threshold", 3.0, "preemption 阈值 (分钟)")
	executeCmd.Flags().BoolP("json", "j", false, "输出原始 JSON 格式")
}

func executeSimulation(cmd *cobra.Command, args []string) {
	initLog()
	log.Printf("[execute] 启动自定义参数仿真")

	newRate, _ := cmd.Flags().GetFloat64("new-rate")
	recheckRate, _ := cmd.Flags().GetFloat64("recheck-rate")
	newService, _ := cmd.Flags().GetFloat64("new-service")
	recheckService, _ := cmd.Flags().GetFloat64("recheck-service")
	duration, _ := cmd.Flags().GetInt("duration")
	preemption, _ := cmd.Flags().GetBool("preemption")
	threshold, _ := cmd.Flags().GetFloat64("threshold")
	outputJSON, _ := cmd.Flags().GetBool("json")

	params := map[string]interface{}{
		"new_patient_arrival_rate": newRate,
		"recheck_arrival_rate":     recheckRate,
		"new_patient_service_time": newService,
		"recheck_service_time":    recheckService,
		"simulation_time":          duration,
		"seed":                     42,
		"preemption_enabled":       preemption,
		"preemption_threshold":     threshold,
	}

	log.Printf("[execute] 参数: new_rate=%.2f, recheck_rate=%.2f, new_service=%.1f, preemption=%v",
		newRate, recheckRate, newService, preemption)

	fmt.Println("正在运行仿真...")
	log.Printf("[execute] 开始执行仿真")
	results, err := callPythonEngine(params)
	if err != nil {
		log.Printf("[execute] 错误: 仿真执行失败: %v", err)
		fmt.Fprintf(os.Stderr, "错误: 仿真执行失败: %v\n", err)
		os.Exit(1)
	}

	// Save to SQLite
	if err := saveToSQLite(results); err != nil {
		log.Printf("[execute] 警告: 保存到数据库失败: %v", err)
	} else {
		fmt.Println("结果已保存到数据库")
	}

	log.Printf("[execute] 仿真完成")
	if outputJSON {
		jsonBytes, _ := json.MarshalIndent(results, "", "  ")
		fmt.Println(string(jsonBytes))
	} else {
		formatResults(results)
	}
	log.Printf("[execute] 命令执行完成")
}

// sensitivityCmd represents the sensitivity analysis command
var sensitivityCmd = &cobra.Command{
	Use:   "sensitivity",
	Short: "敏感性分析",
	Long: `运行参数扫描敏感性分析，生成 CSV 报告和图表。

敏感性分析帮助门诊管理者量化理解「复查插入」策略对关键指标的影响，
包括：平均等待时间、医生利用率、中断次数等。

子命令：
  run       - 对指定变量进行敏感性分析（默认: preemption_threshold）
  threshold - 专门扫描 preemption_threshold 从 1 到 30 分钟

使用示例：
  # 扫描 preemption_threshold 的影响（默认配置）
  sim sensitivity run

  # 自定义扫描配置
  sim sensitivity run --runs-per-value 5 --output-csv results.csv

  # 使用特定配置文件
  sim sensitivity run -c configs/high-recheck.yaml

  # 快速阈值敏感性分析（1-30分钟，步长1分钟）
  sim sensitivity threshold

  # 阈值分析更多配置
  sim sensitivity threshold --runs-per-value 5 --output-csv threshold.csv`,
}

func init() {
	rootCmd.AddCommand(sensitivityCmd)
}

// compareCmd represents the compare command
var compareCmd = &cobra.Command{
	Use:   "compare",
	Short: "对比模式（preemption vs 无 preemption）",
	Long:  `运行两组仿真：「允许preemption」和「禁止preemption」，输出对比表`,
	Run:   runCompare,
}

func init() {
	rootCmd.AddCommand(compareCmd)
	compareCmd.Flags().StringP("config", "c", "", "配置文件路径（默认为 configs/default.yaml）")
	compareCmd.Flags().Int("runs", 10, "每组仿真运行次数")
	compareCmd.Flags().String("output-csv", "outputs/compare_result.csv", "CSV 输出路径")
	compareCmd.Flags().BoolP("json", "j", false, "输出原始 JSON 格式")
}

// runCompare runs comparison between preemption enabled vs disabled
func runCompare(cmd *cobra.Command, args []string) {
	configPath, _ := cmd.Flags().GetString("config")
	runs, _ := cmd.Flags().GetInt("runs")
	outputCSV, _ := cmd.Flags().GetString("output-csv")
	outputJSON, _ := cmd.Flags().GetBool("json")

	// Load config
	var cfg *configs.Config
	var err error

	if configPath != "" {
		cfg, err = configs.Load(configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "错误: 加载配置失败: %v\n", err)
			os.Exit(1)
		}
	} else {
		cfg = configs.GetDefault()
	}

	fmt.Println("正在运行对比仿真...")
	fmt.Printf("每组仿真次数: %d\n\n", runs)

	// Build base params
	baseParams := map[string]interface{}{
		"new_patient_arrival_rate": cfg.NewPatient.ArrivalRate,
		"recheck_arrival_rate":     cfg.RecheckPatient.ArrivalRate,
		"new_patient_service_time": cfg.NewPatient.ServiceTime,
		"recheck_service_time":     cfg.RecheckPatient.ServiceTime,
		"simulation_time":          cfg.Simulation.Duration,
		"seed":                     42,
	}

	// Run with preemption enabled
	paramsWithPreempt := make(map[string]interface{})
	for k, v := range baseParams {
		paramsWithPreempt[k] = v
	}
	paramsWithPreempt["preemption_enabled"] = true
	paramsWithPreempt["preemption_threshold"] = float64(cfg.Preemption.ThresholdMinutes)

	// Run with preemption disabled
	paramsNoPreempt := make(map[string]interface{})
	for k, v := range baseParams {
		paramsNoPreempt[k] = v
	}
	paramsNoPreempt["preemption_enabled"] = false
	paramsNoPreempt["preemption_threshold"] = 0.0

	// Run multiple times and aggregate
	type resultPair struct {
		withPreempt map[string]interface{}
		noPreempt   map[string]interface{}
	}

	results := []resultPair{}

	for i := 0; i < runs; i++ {
		paramsWithPreempt["seed"] = 42 + i
		paramsNoPreempt["seed"] = 42 + i

		resultWith, err1 := callPythonEngine(paramsWithPreempt)
		resultNo, err2 := callPythonEngine(paramsNoPreempt)

		if err1 != nil || err2 != nil {
			fmt.Fprintf(os.Stderr, "警告: 第 %d 次仿真失败: preemption=%v, no-preempt=%v\n", i+1, err1, err2)
			continue
		}

		results = append(results, resultPair{
			withPreempt: resultWith,
			noPreempt:   resultNo,
		})
	}

	if len(results) == 0 {
		fmt.Fprintf(os.Stderr, "错误: 所有仿真均失败\n")
		os.Exit(1)
	}

	// Aggregate results
	aggregate := func(results []map[string]interface{}, field string) float64 {
		sum := 0.0
		for _, r := range results {
			sum += r[field].(float64)
		}
		return sum / float64(len(results))
	}

	// Calculate averages
	avgWaitWithPreempt := aggregate(func() []map[string]interface{} {
		r := make([]map[string]interface{}, len(results))
		for i, v := range results {
			r[i] = v.withPreempt
		}
		return r
	}(), "avg_wait_time")

	avgWaitNoPreempt := aggregate(func() []map[string]interface{} {
		r := make([]map[string]interface{}, len(results))
		for i, v := range results {
			r[i] = v.noPreempt
		}
		return r
	}(), "avg_wait_time")

	newWaitWithPreempt := aggregate(func() []map[string]interface{} {
		r := make([]map[string]interface{}, len(results))
		for i, v := range results {
			r[i] = v.withPreempt
		}
		return r
	}(), "new_patient_avg_wait")

	newWaitNoPreempt := aggregate(func() []map[string]interface{} {
		r := make([]map[string]interface{}, len(results))
		for i, v := range results {
			r[i] = v.noPreempt
		}
		return r
	}(), "new_patient_avg_wait")

	recheckWaitWithPreempt := aggregate(func() []map[string]interface{} {
		r := make([]map[string]interface{}, len(results))
		for i, v := range results {
			r[i] = v.withPreempt
		}
		return r
	}(), "recheck_patient_avg_wait")

	recheckWaitNoPreempt := aggregate(func() []map[string]interface{} {
		r := make([]map[string]interface{}, len(results))
		for i, v := range results {
			r[i] = v.noPreempt
		}
		return r
	}(), "recheck_patient_avg_wait")

	utilWithPreempt := aggregate(func() []map[string]interface{} {
		r := make([]map[string]interface{}, len(results))
		for i, v := range results {
			r[i] = v.withPreempt
		}
		return r
	}(), "server_utilization")

	utilNoPreempt := aggregate(func() []map[string]interface{} {
		r := make([]map[string]interface{}, len(results))
		for i, v := range results {
			r[i] = v.noPreempt
		}
		return r
	}(), "server_utilization")

	preemptCountAvg := aggregate(func() []map[string]interface{} {
		r := make([]map[string]interface{}, len(results))
		for i, v := range results {
			r[i] = v.withPreempt
		}
		return r
	}(), "preemption_count")

	totalPatientsAvg := aggregate(func() []map[string]interface{} {
		r := make([]map[string]interface{}, len(results))
		for i, v := range results {
			r[i] = v.withPreempt
		}
		return r
	}(), "total_patients")

	queueLenWithPreempt := aggregate(func() []map[string]interface{} {
		r := make([]map[string]interface{}, len(results))
		for i, v := range results {
			r[i] = v.withPreempt
		}
		return r
	}(), "avg_queue_length")

	queueLenNoPreempt := aggregate(func() []map[string]interface{} {
		r := make([]map[string]interface{}, len(results))
		for i, v := range results {
			r[i] = v.noPreempt
		}
		return r
	}(), "avg_queue_length")

	// Calculate changes
	avgWaitChange := avgWaitNoPreempt - avgWaitWithPreempt
	newWaitChange := newWaitNoPreempt - newWaitWithPreempt
	recheckWaitChange := recheckWaitNoPreempt - recheckWaitWithPreempt
	queueLenChange := queueLenNoPreempt - queueLenWithPreempt

	changePct := func(newVal, oldVal float64) float64 {
		if oldVal == 0 {
			return 0
		}
		return (newVal - oldVal) / oldVal * 100
	}

	// Prepare comparison result
	comparisonResult := map[string]interface{}{
		"runs":                  runs,
		"successful_runs":      len(results),
		"with_preemption": map[string]interface{}{
			"avg_wait_time":       avgWaitWithPreempt,
			"new_patient_wait":    newWaitWithPreempt,
			"recheck_patient_wait": recheckWaitWithPreempt,
			"server_utilization":  utilWithPreempt,
			"avg_queue_length":    queueLenWithPreempt,
			"preemption_count":    preemptCountAvg,
			"total_patients":      totalPatientsAvg,
		},
		"without_preemption": map[string]interface{}{
			"avg_wait_time":       avgWaitNoPreempt,
			"new_patient_wait":    newWaitNoPreempt,
			"recheck_patient_wait": recheckWaitNoPreempt,
			"server_utilization":  utilNoPreempt,
			"avg_queue_length":    queueLenNoPreempt,
			"preemption_count":    0.0,
			"total_patients":      totalPatientsAvg,
		},
		"changes": map[string]interface{}{
			"avg_wait_time":       avgWaitChange,
			"avg_wait_time_pct":   changePct(avgWaitWithPreempt, avgWaitNoPreempt),
			"new_patient_wait":    newWaitChange,
			"new_patient_wait_pct": changePct(newWaitWithPreempt, newWaitNoPreempt),
			"recheck_patient_wait": recheckWaitChange,
			"recheck_patient_wait_pct": changePct(recheckWaitWithPreempt, recheckWaitNoPreempt),
			"avg_queue_length":    queueLenChange,
			"avg_queue_length_pct": changePct(queueLenWithPreempt, queueLenNoPreempt),
		},
	}

	// Output
	if outputJSON {
		jsonBytes, _ := json.MarshalIndent(comparisonResult, "", "  ")
		fmt.Println(string(jsonBytes))
	} else {
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "\n=== Preemption 对比分析 ===")
		fmt.Fprintf(w, "仿真次数: %d 成功: %d\n", runs, len(results))
		fmt.Fprintln(w, "\n指标\t禁止 Preemption\t允许 Preemption\t变化\t变化率")
		fmt.Fprintln(w, "----\t----\t----\t----\t----")
		fmt.Fprintf(w, "平均等待时间\t%.2f 分钟\t%.2f 分钟\t%.2f\t%.1f%%\n",
			avgWaitNoPreempt, avgWaitWithPreempt, avgWaitChange, changePct(avgWaitWithPreempt, avgWaitNoPreempt))
		fmt.Fprintf(w, "新诊平均等待\t%.2f 分钟\t%.2f 分钟\t%.2f\t%.1f%%\n",
			newWaitNoPreempt, newWaitWithPreempt, newWaitChange, changePct(newWaitWithPreempt, newWaitNoPreempt))
		fmt.Fprintf(w, "复查平均等待\t%.2f 分钟\t%.2f 分钟\t%.2f\t%.1f%%\n",
			recheckWaitNoPreempt, recheckWaitWithPreempt, recheckWaitChange, changePct(recheckWaitWithPreempt, recheckWaitNoPreempt))
		fmt.Fprintf(w, "平均队列长度\t%.2f\t%.2f\t%.2f\t%.1f%%\n",
			queueLenNoPreempt, queueLenWithPreempt, queueLenChange, changePct(queueLenWithPreempt, queueLenNoPreempt))
		fmt.Fprintf(w, "医生利用率\t%.1f%%\t%.1f%%\t-\t-\n",
			utilNoPreempt*100, utilWithPreempt*100)
		fmt.Fprintf(w, "平均中断次数\t0\t%.1f\t-\t-\n", preemptCountAvg)
		fmt.Fprintln(w, "----\t----\t----\t----\t----")

		// Analysis
		fmt.Fprintln(w, "\n=== 分析结论 ===")
		if avgWaitWithPreempt < avgWaitNoPreempt {
			fmt.Fprintf(w, "✓ 启用 Preemption 后总体平均等待时间减少 %.2f 分钟 (%.1f%%)\n",
				-avgWaitChange, -changePct(avgWaitWithPreempt, avgWaitNoPreempt))
		} else {
			fmt.Fprintf(w, "✗ 启用 Preemption 后总体平均等待时间增加 %.2f 分钟 (%.1f%%)\n",
				avgWaitChange, changePct(avgWaitWithPreempt, avgWaitNoPreempt))
		}

		if newWaitWithPreempt > newWaitNoPreempt {
			fmt.Fprintf(w, "  - 新诊患者等待时间增加 %.2f 分钟（被中断影响）\n", -newWaitChange)
		} else {
			fmt.Fprintf(w, "  - 新诊患者等待时间减少 %.2f 分钟\n", -newWaitChange)
		}

		if recheckWaitWithPreempt < recheckWaitNoPreempt {
			fmt.Fprintf(w, "  - 复查患者等待时间减少 %.2f 分钟（优先权生效）\n", -recheckWaitChange)
		} else {
			fmt.Fprintf(w, "  - 复查患者等待时间增加 %.2f 分钟\n", -recheckWaitChange)
		}

		w.Flush()

		// Recommendation
		fmt.Println("\n=== 建议 ===")
		if avgWaitWithPreempt < avgWaitNoPreempt*0.95 { // 5% improvement threshold
			fmt.Println("建议启用 Preemption")
			fmt.Println("理由: 允许复查插入可减少总体平均等待时间")
		} else {
			fmt.Println("建议禁用 Preemption（当前参数下改善不明显）")
			fmt.Println("理由: 复查插队带来的收益小于对新诊患者的干扰")
		}
	}

	// Write CSV
	if err := writeCompareCSV(outputCSV, comparisonResult); err != nil {
		fmt.Fprintf(os.Stderr, "警告: CSV 输出失败: %v\n", err)
	} else {
		fmt.Printf("\nCSV 结果已保存: %s\n", outputCSV)
	}
}

// writeCompareCSV writes comparison results to CSV
func writeCompareCSV(csvPath string, result map[string]interface{}) error {
	if err := os.MkdirAll("outputs", 0755); err != nil {
		return fmt.Errorf("创建输出目录失败: %w", err)
	}

	file, err := os.Create(csvPath)
	if err != nil {
		return fmt.Errorf("创建 CSV 文件失败: %w", err)
	}
	defer file.Close()

	fmt.Fprintln(file, "对比项,禁止 Preemption,允许 Preemption,变化,变化率")
	fmt.Fprintln(file, "----,----,----,----,----")

	withPreempt := result["with_preemption"].(map[string]interface{})
	noPreempt := result["without_preemption"].(map[string]interface{})
	changes := result["changes"].(map[string]interface{})

	writeRow := func(name string, noVal, withVal, change, changePct float64) {
		fmt.Fprintf(file, "%s,%.2f,%.2f,%.2f,%.1f%%\n", name, noVal, withVal, change, changePct)
	}

	writeRow("平均等待时间",
		noPreempt["avg_wait_time"].(float64),
		withPreempt["avg_wait_time"].(float64),
		changes["avg_wait_time"].(float64),
		changes["avg_wait_time_pct"].(float64))

	writeRow("新诊平均等待",
		noPreempt["new_patient_wait"].(float64),
		withPreempt["new_patient_wait"].(float64),
		changes["new_patient_wait"].(float64),
		changes["new_patient_wait_pct"].(float64))

	writeRow("复查平均等待",
		noPreempt["recheck_patient_wait"].(float64),
		withPreempt["recheck_patient_wait"].(float64),
		changes["recheck_patient_wait"].(float64),
		changes["recheck_patient_wait_pct"].(float64))

	writeRow("平均队列长度",
		noPreempt["avg_queue_length"].(float64),
		withPreempt["avg_queue_length"].(float64),
		changes["avg_queue_length"].(float64),
		changes["avg_queue_length_pct"].(float64))

	fmt.Fprintf(file, "医生利用率,%.1f%%,%.1f%%,-,-\n",
		noPreempt["server_utilization"].(float64)*100,
		withPreempt["server_utilization"].(float64)*100)

	fmt.Fprintf(file, "平均中断次数,0,%.0f,-,-\n", withPreempt["preemption_count"].(float64))
	fmt.Fprintf(file, "总患者数,%.0f,%.0f,-,-\n", noPreempt["total_patients"].(float64), withPreempt["total_patients"].(float64))

	return nil
}

// sensitivityRunCmd represents the sensitivity run subcommand
var sensitivityRunCmd = &cobra.Command{
	Use:   "run",
	Short: "运行敏感性分析",
	Long: `调用 Python runner 进行参数扫描，然后生成图表。

扫描指定变量在不同取值下的仿真结果，分析其对关键指标的影响。

参数说明：
  --scan-variable 扫描的变量名，支持：preemption_threshold、new_patient_arrival_rate、
                   recheck_arrival_rate、new_patient_service_time 等

扫描值范围（默认）：
  preemption_threshold: [0, 3, 5, 7, 10, 15]
  其他变量根据配置动态确定

使用示例：
  # 使用默认配置扫描 preemption_threshold
  sim sensitivity run

  # 扫描复查到达率的影响
  sim sensitivity run --scan-variable recheck_arrival_rate

  # 自定义扫描：每个值运行 5 次
  sim sensitivity run --runs-per-value 5

  # 指定输入输出路径
  sim sensitivity run --config configs/high-recheck.yaml --output-csv mydata.csv`,
	Run:   runSensitivityAnalysis,
}

func init() {
	sensitivityCmd.AddCommand(sensitivityRunCmd)
	sensitivityRunCmd.Flags().StringP("config", "c", "", "配置文件路径（默认为 configs/default.yaml）")
	sensitivityRunCmd.Flags().Int("runs-per-value", 10, "每个扫描值的重复次数")
	sensitivityRunCmd.Flags().String("scan-variable", "preemption_threshold", "扫描变量名")
	sensitivityRunCmd.Flags().String("output-csv", "outputs/sensitivity.csv", "CSV 输出路径")
	sensitivityRunCmd.Flags().String("output-chart", "outputs/sensitivity_chart.png", "图表输出路径")
}

// sensitivityThresholdCmd represents the threshold sensitivity subcommand
var sensitivityThresholdCmd = &cobra.Command{
	Use:   "threshold",
	Short: "Preemption阈值敏感性分析",
	Long: `扫描 preemption_threshold 从 1 到 30 分钟（步长1分钟），分析对平均等待时间的影响。

这是敏感性分析的一个特例，专门针对 preemption_threshold 参数。
阈值含义：只有当患者已接受服务的时间超过此阈值时，才允许被高优先级患者中断。

阈值设计建议：
  - 低阈值(1-3分钟)：复查患者更容易插入，但新诊患者服务频繁中断
  - 中等阈值(5-10分钟)：平衡复查等待和新诊稳定性
  - 高阈值(15+分钟)：新诊患者服务更稳定，但复查等待时间增加

使用示例：
  # 使用默认配置运行阈值敏感性分析
  sim sensitivity threshold

  # 每个阈值运行 5 次仿真（更稳定的结果）
  sim sensitivity threshold --runs-per-value 5

  # 指定输出路径
  sim sensitivity threshold --output-csv t.csv --output-chart t.png`,
	Run:   runThresholdSensitivity,
}

func init() {
	sensitivityCmd.AddCommand(sensitivityThresholdCmd)
	sensitivityThresholdCmd.Flags().StringP("config", "c", "", "配置文件路径（默认为 configs/default.yaml）")
	sensitivityThresholdCmd.Flags().Int("runs-per-value", 10, "每个扫描值的重复次数")
	sensitivityThresholdCmd.Flags().String("output-csv", "outputs/threshold_sensitivity.csv", "CSV 输出路径")
	sensitivityThresholdCmd.Flags().String("output-chart", "outputs/threshold_sensitivity.png", "图表输出路径")
}

// sensitivityServiceTimeCmd represents the service time sensitivity subcommand
var sensitivityServiceTimeCmd = &cobra.Command{
	Use:   "service-time",
	Short: "服务时长敏感性分析",
	Long: `扫描新诊服务时长从 5 到 30 分钟（步长5分钟），分析对平均等待时间的影响。

复查服务时长保持不变（来自配置文件的值）。
此分析帮助门诊管理者了解「服务时间变化」对排队等待的影响。

服务时长设计建议：
  - 短服务时长(5-10分钟)：高流转，如普通门诊
  - 中等服务时长(10-15分钟)：常规门诊，如专家门诊
  - 长服务时长(15-30分钟)：复杂诊疗，如特需门诊

使用示例：
  # 使用默认配置运行服务时长敏感性分析
  sim sensitivity service-time

  # 自定义扫描范围（步长2分钟）
  sim sensitivity service-time --step 2 --min 5 --max 25

  # 每个服务时长运行 5 次仿真
  sim sensitivity service-time --runs-per-value 5

  # 指定输出路径
  sim sensitivity service-time --output-csv st.csv --output-chart st.png`,
	Run:   runServiceTimeSensitivity,
}

func init() {
	sensitivityCmd.AddCommand(sensitivityServiceTimeCmd)
	sensitivityServiceTimeCmd.Flags().StringP("config", "c", "", "配置文件路径（默认为 configs/default.yaml）")
	sensitivityServiceTimeCmd.Flags().Int("runs-per-value", 10, "每个扫描值的重复次数")
	sensitivityServiceTimeCmd.Flags().Int("step", 5, "服务时长扫描步长（分钟）")
	sensitivityServiceTimeCmd.Flags().Int("min", 5, "服务时长最小值（分钟）")
	sensitivityServiceTimeCmd.Flags().Int("max", 30, "服务时长最大值（分钟）")
	sensitivityServiceTimeCmd.Flags().String("output-csv", "outputs/service_time_sensitivity.csv", "CSV 输出路径")
	sensitivityServiceTimeCmd.Flags().String("output-chart", "outputs/service_time_sensitivity.png", "图表输出路径")
}

// runSensitivityAnalysis runs the sensitivity analysis
func runSensitivityAnalysis(cmd *cobra.Command, args []string) {
	configPath, _ := cmd.Flags().GetString("config")
	runsPerValue, _ := cmd.Flags().GetInt("runs-per-value")
	scanVariable, _ := cmd.Flags().GetString("scan-variable")
	outputCSV, _ := cmd.Flags().GetString("output-csv")
	outputChart, _ := cmd.Flags().GetString("output-chart")

	// Load config
	var cfg *configs.Config
	var err error

	if configPath != "" {
		cfg, err = configs.Load(configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "错误: 加载配置失败: %v\n", err)
			os.Exit(1)
		}
	} else {
		cfg = configs.GetDefault()
	}

	// Build base params
	baseParams := map[string]interface{}{
		"new_patient_arrival_rate": cfg.NewPatient.ArrivalRate,
		"recheck_arrival_rate":     cfg.RecheckPatient.ArrivalRate,
		"new_patient_service_time": cfg.NewPatient.ServiceTime,
		"recheck_service_time":     cfg.RecheckPatient.ServiceTime,
		"simulation_time":          cfg.Simulation.Duration,
		"seed":                     42,
		"preemption_enabled":       cfg.Preemption.Enabled,
	}

	// Build runner input JSON - use concurrent runner
	runnerInput := map[string]interface{}{
		"base_params":    baseParams,
		"scan_variable":   scanVariable,
		"scan_values":    []float64{0, 3, 5, 7, 10, 15},
		"runs_per_value": runsPerValue,
		"output_csv":     outputCSV,
		"parallel":      true, // Enable Go-level concurrency
	}

	// Serialize to JSON
	jsonData, err := json.Marshal(runnerInput)
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: 序列化参数失败: %v\n", err)
		os.Exit(1)
	}

	// Call runner.py with concurrent execution
	fmt.Println("正在运行敏感性分析（并发模式）...")
	result, err := callPythonRunnerConcurrently(string(jsonData))
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: 敏感性分析失败: %v\n", err)
		os.Exit(1)
	}

	// Print summary
	successfulRuns := toInt(result["successful_runs"])
	totalRuns := toInt(result["total_runs"])
	fmt.Printf("敏感性分析完成: %d/%d 仿真成功\n", successfulRuns, totalRuns)
	fmt.Printf("CSV 输出: %s\n", outputCSV)

	// Generate chart
	generateChart(outputCSV, outputChart, scanVariable, "敏感性分析结果")

	// Display aggregated results
	displayAggregatedResults(result, scanVariable)
}

// toInt safely converts interface{} to int
func toInt(v interface{}) int {
	switch val := v.(type) {
	case float64:
		return int(val)
	case int:
		return val
	case int64:
		return int(val)
	default:
		return 0
	}
}

// runThresholdSensitivity runs threshold sensitivity analysis
func runThresholdSensitivity(cmd *cobra.Command, args []string) {
	configPath, _ := cmd.Flags().GetString("config")
	runsPerValue, _ := cmd.Flags().GetInt("runs-per-value")
	outputCSV, _ := cmd.Flags().GetString("output-csv")
	outputChart, _ := cmd.Flags().GetString("output-chart")

	// Load config
	var cfg *configs.Config
	var err error

	if configPath != "" {
		cfg, err = configs.Load(configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "错误: 加载配置失败: %v\n", err)
			os.Exit(1)
		}
	} else {
		cfg = configs.GetDefault()
	}

	// Build base params
	baseParams := map[string]interface{}{
		"new_patient_arrival_rate": cfg.NewPatient.ArrivalRate,
		"recheck_arrival_rate":     cfg.RecheckPatient.ArrivalRate,
		"new_patient_service_time": cfg.NewPatient.ServiceTime,
		"recheck_service_time":     cfg.RecheckPatient.ServiceTime,
		"simulation_time":          cfg.Simulation.Duration,
		"seed":                     42,
		"preemption_enabled":       true,
	}

	// Build runner input JSON - scan threshold from 1 to 30 minutes (step 1)
	scanValues := make([]float64, 30)
	for i := 0; i < 30; i++ {
		scanValues[i] = float64(i + 1)
	}

	runnerInput := map[string]interface{}{
		"base_params":    baseParams,
		"scan_variable":   "preemption_threshold",
		"scan_values":    scanValues,
		"runs_per_value": runsPerValue,
		"output_csv":     outputCSV,
	}

	// Serialize to JSON
	jsonData, err := json.Marshal(runnerInput)
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: 序列化参数失败: %v\n", err)
		os.Exit(1)
	}

	// Find runner.py path
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: 获取可执行文件路径失败: %v\n", err)
		os.Exit(1)
	}
	execDir := strings.TrimSuffix(exe, "\\bin\\sim.exe")
	runnerPath := execDir + "\\sim\\runner.py"

	if _, err := os.Stat(runnerPath); os.IsNotExist(err) {
		runnerPath = "sim/runner.py"
		if _, err := os.Stat(runnerPath); os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "错误: 找不到 sim/runner.py\n")
			os.Exit(1)
		}
	}

	// Call runner.py
	fmt.Println("正在运行 Preemption 阈值敏感性分析...")
	fmt.Println("扫描范围: threshold = 1~30 分钟（步长 1 分钟）")
	pythonCmd := getPythonCommand()
	cmdRunner := exec.Command(pythonCmd, runnerPath)
	cmdRunner.Stdin = strings.NewReader(string(jsonData))

	output, err := cmdRunner.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			fmt.Fprintf(os.Stderr, "错误: Python runner 执行失败: %s\n", string(exitErr.Stderr))
		} else {
			fmt.Fprintf(os.Stderr, "错误: 调用 Python runner 失败: %v\n", err)
		}
		os.Exit(1)
	}

	// Parse runner output to get results
	var runnerResult map[string]interface{}
	if err := json.Unmarshal(output, &runnerResult); err != nil {
		fmt.Fprintf(os.Stderr, "错误: 解析 runner 输出失败: %v\n", err)
		os.Exit(1)
	}

	// Print summary
	successfulRuns := int(runnerResult["successful_runs"].(float64))
	totalRuns := int(runnerResult["total_runs"].(float64))
	fmt.Printf("敏感性分析完成: %d/%d 仿真成功\n", successfulRuns, totalRuns)
	fmt.Printf("CSV 输出: %s\n", outputCSV)

	// Now call visualize.py to generate chart
	visualizeInput := map[string]interface{}{
		"csv_path":     outputCSV,
		"output_path":  outputChart,
		"scan_variable": "preemption_threshold",
		"title":        "Preemption 阈值敏感性分析",
	}

	vizJSONData, err := json.Marshal(visualizeInput)
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: 序列化可视化参数失败: %v\n", err)
		os.Exit(1)
	}

	vizPath := execDir + "\\sim\\visualize.py"
	if _, err := os.Stat(vizPath); os.IsNotExist(err) {
		vizPath = "sim/visualize.py"
	}

	fmt.Println("正在生成图表...")
	cmdViz := exec.Command(pythonCmd, vizPath)
	cmdViz.Stdin = strings.NewReader(string(vizJSONData))

	vizOutput, err := cmdViz.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			fmt.Fprintf(os.Stderr, "警告: 图表生成失败: %s\n", string(exitErr.Stderr))
		}
	} else {
		fmt.Printf("图表生成完成: %s\n", outputChart)
		_ = string(vizOutput)
	}

	// Display aggregated results in table format
	if aggregated, ok := runnerResult["aggregated_results"].([]interface{}); ok && len(aggregated) > 0 {
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "\n=== Preemption 阈值敏感性分析 ===")
		fmt.Fprintf(w, "阈值(分钟)\t平均等待\t新诊等待\t复查等待\t医生利用率\t中断次数\n")
		fmt.Fprintln(w, "----\t----\t----\t----\t----\t----")
		for _, item := range aggregated {
			row := item.(map[string]interface{})
			threshold := row["preemption_threshold"]
			fmt.Fprintf(w, "%.0f\t%.2f\t%.2f\t%.2f\t%.1f%%\t%.0f\n",
				threshold,
				row["avg_wait_time"],
				row["new_patient_avg_wait"],
				row["recheck_patient_avg_wait"],
				row["server_utilization"].(float64)*100,
				row["preemption_count"])
		}
		w.Flush()

		// Find optimal threshold (lowest avg wait time)
		bestThreshold := 0.0
		bestWait := aggregated[0].(map[string]interface{})["avg_wait_time"].(float64)
		baselineWait := aggregated[0].(map[string]interface{})["avg_wait_time"].(float64) // threshold=1 is baseline

		for _, item := range aggregated {
			row := item.(map[string]interface{})
			waitTime := row["avg_wait_time"].(float64)
			if waitTime < bestWait {
				bestWait = waitTime
				bestThreshold = row["preemption_threshold"].(float64)
			}
		}

		// Calculate improvement percentage
		improvement := 0.0
		if baselineWait > 0 {
			improvement = (baselineWait - bestWait) / baselineWait * 100
		}

		// Output recommendation
		fmt.Println("\n=== 最优阈值推荐 ===")
		fmt.Printf("最优阈值: %.0f 分钟（平均等待时间: %.2f 分钟）\n", bestThreshold, bestWait)
		fmt.Printf("相对阈值=1分钟时改善: %.1f%%\n", improvement)

		// Analyze trend
		if len(aggregated) >= 3 {
			firstWait := aggregated[0].(map[string]interface{})["avg_wait_time"].(float64)
			lastWait := aggregated[len(aggregated)-1].(map[string]interface{})["avg_wait_time"].(float64)
			if lastWait < firstWait {
				fmt.Println("趋势: 随着阈值增加，等待时间减少（新诊被中断次数减少）")
			} else if lastWait > firstWait {
				fmt.Println("趋势: 随着阈值增加，等待时间增加（复查插队效果减弱）")
			} else {
				fmt.Println("趋势: 阈值变化对等待时间影响不大")
			}
		}
	}
}

// runServiceTimeSensitivity runs service time sensitivity analysis
func runServiceTimeSensitivity(cmd *cobra.Command, args []string) {
	configPath, _ := cmd.Flags().GetString("config")
	runsPerValue, _ := cmd.Flags().GetInt("runs-per-value")
	step, _ := cmd.Flags().GetInt("step")
	minService, _ := cmd.Flags().GetInt("min")
	maxService, _ := cmd.Flags().GetInt("max")
	outputCSV, _ := cmd.Flags().GetString("output-csv")
	outputChart, _ := cmd.Flags().GetString("output-chart")

	// Load config
	var cfg *configs.Config
	var err error

	if configPath != "" {
		cfg, err = configs.Load(configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "错误: 加载配置失败: %v\n", err)
			os.Exit(1)
		}
	} else {
		cfg = configs.GetDefault()
	}

	// Build base params
	baseParams := map[string]interface{}{
		"new_patient_arrival_rate": cfg.NewPatient.ArrivalRate,
		"recheck_arrival_rate":     cfg.RecheckPatient.ArrivalRate,
		"new_patient_service_time": cfg.NewPatient.ServiceTime,
		"recheck_service_time":     cfg.RecheckPatient.ServiceTime,
		"simulation_time":          cfg.Simulation.Duration,
		"seed":                     42,
		"preemption_enabled":       cfg.Preemption.Enabled,
		"preemption_threshold":     float64(cfg.Preemption.ThresholdMinutes),
	}

	// Build scan values based on step, min, max
	scanValues := make([]float64, 0)
	for v := minService; v <= maxService; v += step {
		scanValues = append(scanValues, float64(v))
	}

	runnerInput := map[string]interface{}{
		"base_params":     baseParams,
		"scan_variable":    "new_patient_service_time",
		"scan_values":      scanValues,
		"runs_per_value":  runsPerValue,
		"output_csv":      outputCSV,
	}

	// Serialize to JSON
	jsonData, err := json.Marshal(runnerInput)
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: 序列化参数失败: %v\n", err)
		os.Exit(1)
	}

	// Find runner.py path
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: 获取可执行文件路径失败: %v\n", err)
		os.Exit(1)
	}
	execDir := strings.TrimSuffix(exe, "\\bin\\sim.exe")
	runnerPath := execDir + "\\sim\\runner.py"

	if _, err := os.Stat(runnerPath); os.IsNotExist(err) {
		runnerPath = "sim/runner.py"
		if _, err := os.Stat(runnerPath); os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "错误: 找不到 sim/runner.py\n")
			os.Exit(1)
		}
	}

	// Call runner.py
	fmt.Println("正在运行服务时长敏感性分析...")
	fmt.Printf("扫描范围: new_patient_service_time = %d~%d 分钟（步长 %d 分钟）\n", minService, maxService, step)
	pythonCmd := getPythonCommand()
	cmdRunner := exec.Command(pythonCmd, runnerPath)
	cmdRunner.Stdin = strings.NewReader(string(jsonData))

	output, err := cmdRunner.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			fmt.Fprintf(os.Stderr, "错误: Python runner 执行失败: %s\n", string(exitErr.Stderr))
		} else {
			fmt.Fprintf(os.Stderr, "错误: 调用 Python runner 失败: %v\n", err)
		}
		os.Exit(1)
	}

	// Parse runner output to get results
	var runnerResult map[string]interface{}
	if err := json.Unmarshal(output, &runnerResult); err != nil {
		fmt.Fprintf(os.Stderr, "错误: 解析 runner 输出失败: %v\n", err)
		os.Exit(1)
	}

	// Print summary
	successfulRuns := int(runnerResult["successful_runs"].(float64))
	totalRuns := int(runnerResult["total_runs"].(float64))
	fmt.Printf("敏感性分析完成: %d/%d 仿真成功\n", successfulRuns, totalRuns)
	fmt.Printf("CSV 输出: %s\n", outputCSV)

	// Now call visualize.py to generate chart
	visualizeInput := map[string]interface{}{
		"csv_path":      outputCSV,
		"output_path":   outputChart,
		"scan_variable": "new_patient_service_time",
		"title":         "新诊服务时长敏感性分析",
	}

	vizJSONData, err := json.Marshal(visualizeInput)
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: 序列化可视化参数失败: %v\n", err)
		os.Exit(1)
	}

	vizPath := execDir + "\\sim\\visualize.py"
	if _, err := os.Stat(vizPath); os.IsNotExist(err) {
		vizPath = "sim/visualize.py"
	}

	fmt.Println("正在生成图表...")
	cmdViz := exec.Command(pythonCmd, vizPath)
	cmdViz.Stdin = strings.NewReader(string(vizJSONData))

	vizOutput, err := cmdViz.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			fmt.Fprintf(os.Stderr, "警告: 图表生成失败: %s\n", string(exitErr.Stderr))
		}
	} else {
		fmt.Printf("图表生成完成: %s\n", outputChart)
		_ = string(vizOutput)
	}

	// Display aggregated results in table format
	if aggregated, ok := runnerResult["aggregated_results"].([]interface{}); ok && len(aggregated) > 0 {
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "\n=== 新诊服务时长敏感性分析 ===")
		fmt.Fprintf(w, "服务时长(分钟)\t平均等待\t新诊等待\t复查等待\t医生利用率\t中断次数\n")
		fmt.Fprintln(w, "----\t----\t----\t----\t----\t----")
		for _, item := range aggregated {
			row := item.(map[string]interface{})
			serviceTime := row["new_patient_service_time"]
			fmt.Fprintf(w, "%.0f\t%.2f\t%.2f\t%.2f\t%.1f%%\t%.0f\n",
				serviceTime,
				row["avg_wait_time"],
				row["new_patient_avg_wait"],
				row["recheck_patient_avg_wait"],
				row["server_utilization"].(float64)*100,
				row["preemption_count"])
		}
		w.Flush()

		// Find optimal service time (lowest avg wait time)
		bestServiceTime := 0.0
		bestWait := aggregated[0].(map[string]interface{})["avg_wait_time"].(float64)
		firstWait := bestWait

		for _, item := range aggregated {
			row := item.(map[string]interface{})
			waitTime := row["avg_wait_time"].(float64)
			if waitTime <= bestWait {
				bestWait = waitTime
				bestServiceTime = row["new_patient_service_time"].(float64)
			}
		}

		// Calculate improvement percentage
		improvement := 0.0
		if firstWait > 0 {
			improvement = (firstWait - bestWait) / firstWait * 100
		}

		// Output recommendation
		fmt.Println("\n=== 最优服务时长推荐 ===")
		fmt.Printf("最优服务时长: %.0f 分钟（平均等待时间: %.2f 分钟）\n", bestServiceTime, bestWait)
		fmt.Printf("相对服务时长=%d 分钟时改善: %.1f%%\n", minService, improvement)

		// Analyze trend
		if len(aggregated) >= 3 {
			lastWait := aggregated[len(aggregated)-1].(map[string]interface{})["avg_wait_time"].(float64)
			if lastWait < firstWait {
				fmt.Println("趋势: 随着服务时长增加，等待时间减少（到达率相对降低）")
			} else if lastWait > firstWait {
				fmt.Println("趋势: 随着服务时长增加，等待时间增加（服务能力下降）")
			} else {
				fmt.Println("趋势: 服务时长变化对等待时间影响不大")
			}
		}
	}
}

// simulateCmd represents the simulate command
var simulateCmd = &cobra.Command{
	Use:   "simulate",
	Short: "运行单次仿真",
	Long: `从指定配置文件读取参数，运行 SimPy 仿真，输出结果到 CLI 和 CSV 文件。

与 run 命令的区别：
  - run: 输出格式化的 CLI 表格
  - simulate: 同时输出到 CLI 表格和 CSV 文件（便于后续分析）

使用示例：
  # 使用默认配置运行仿真
  sim simulate

  # 指定配置文件
  sim simulate -c configs/high-recheck.yaml

  # 指定 CSV 输出路径
  sim simulate -o outputs/my_result.csv

  # 输出 JSON 格式（便于程序解析）
  sim simulate -j -c configs/default.yaml`,
	Run:   runSimulate,
}

func init() {
	rootCmd.AddCommand(simulateCmd)
	simulateCmd.Flags().StringP("config", "c", "configs/default.yaml", "配置文件路径")
	simulateCmd.Flags().StringP("output-csv", "o", "", "CSV 输出路径（默认为 outputs/simulate_result.csv）")
	simulateCmd.Flags().BoolP("json", "j", false, "输出原始 JSON 格式")
}

// runSimulate runs a single simulation from config file
func runSimulate(cmd *cobra.Command, args []string) {
	initLog()
	log.Printf("[simulate] 启动单次仿真命令")

	configPath, _ := cmd.Flags().GetString("config")
	outputCSV, _ := cmd.Flags().GetString("output-csv")
	outputJSON, _ := cmd.Flags().GetBool("json")

	if outputCSV == "" {
		outputCSV = "outputs/simulate_result.csv"
	}

	log.Printf("[simulate] 使用配置文件: %s", configPath)

	// Load config
	cfg, err := configs.Load(configPath)
	if err != nil {
		log.Printf("[simulate] 错误: 加载配置失败: %v", err)
		fmt.Fprintf(os.Stderr, "错误: 加载配置失败: %v\n", err)
		os.Exit(1)
	}

	// Build simulation params
	params := buildSimParams(cfg, cfg.Preemption.Enabled, cfg.Preemption.ThresholdMinutes)

	// Run simulation
	fmt.Println("正在运行仿真...")
	log.Printf("[simulate] 开始执行仿真")
	results, err := callPythonEngine(params)
	if err != nil {
		log.Printf("[simulate] 错误: 仿真执行失败: %v", err)
		fmt.Fprintf(os.Stderr, "错误: 仿真执行失败: %v\n", err)
		os.Exit(1)
	}

	// Write CSV output
	log.Printf("[simulate] 保存 CSV 到: %s", outputCSV)
	if err := writeCSVResult(outputCSV, results); err != nil {
		log.Printf("[simulate] 警告: CSV 输出失败: %v", err)
		fmt.Fprintf(os.Stderr, "警告: CSV 输出失败: %v\n", err)
	} else {
		fmt.Printf("CSV 结果已保存: %s\n", outputCSV)
	}

	// Save to SQLite
	if err := saveToSQLite(results); err != nil {
		log.Printf("[simulate] 警告: 保存到数据库失败: %v", err)
	} else {
		fmt.Println("结果已保存到数据库")
	}

	// Output results
	if outputJSON {
		jsonBytes, _ := json.MarshalIndent(results, "", "  ")
		fmt.Println(string(jsonBytes))
	} else {
		formatResults(results)
	}
	log.Printf("[simulate] 命令执行完成")
}

// writeCSVResult writes simulation results to CSV file
func writeCSVResult(csvPath string, results map[string]interface{}) error {
	// Ensure outputs directory exists
	if err := os.MkdirAll("outputs", 0755); err != nil {
		return fmt.Errorf("创建输出目录失败: %w", err)
	}

	// Open CSV file
	file, err := os.Create(csvPath)
	if err != nil {
		return fmt.Errorf("创建 CSV 文件失败: %w", err)
	}
	defer file.Close()

	// Write header
	fmt.Fprintln(file, "参数,值")

	// Write parameters
	params, ok := results["parameters"].(map[string]interface{})
	if ok {
		fmt.Fprintf(file, "新诊到达率,%.4f\n", params["new_patient_arrival_rate"])
		fmt.Fprintf(file, "复查到达率,%.4f\n", params["recheck_arrival_rate"])
		fmt.Fprintf(file, "新诊服务时长,%.2f\n", params["new_patient_service_time"])
		fmt.Fprintf(file, "复查服务时长,%.2f\n", params["recheck_service_time"])
		fmt.Fprintf(file, "仿真时长,%d\n", int(params["simulation_time"].(float64)))
		fmt.Fprintf(file, "Preemption启用,%v\n", params["preemption_enabled"])
		if params["preemption_enabled"].(bool) {
			fmt.Fprintf(file, "Preemption阈值,%.1f\n", params["preemption_threshold"])
		}
	}

	// Write statistics
	fmt.Fprintf(file, "总患者数,%d\n", int(results["total_patients"].(float64)))
	fmt.Fprintf(file, "新诊患者数,%d\n", int(results["new_patients"].(float64)))
	fmt.Fprintf(file, "复查患者数,%d\n", int(results["recheck_patients"].(float64)))
	fmt.Fprintf(file, "平均等待时间,%.2f\n", results["avg_wait_time"])
	fmt.Fprintf(file, "最大等待时间,%.2f\n", results["max_wait_time"])
	fmt.Fprintf(file, "平均队列长度,%.2f\n", results["avg_queue_length"])
	fmt.Fprintf(file, "最大队列长度,%d\n", int(results["max_queue_length"].(float64)))
	fmt.Fprintf(file, "医生利用率,%.4f\n", results["server_utilization"].(float64))
	fmt.Fprintf(file, "新诊平均等待,%.2f\n", results["new_patient_avg_wait"])
	fmt.Fprintf(file, "复查平均等待,%.2f\n", results["recheck_patient_avg_wait"])
	fmt.Fprintf(file, "中断次数,%d\n", int(results["preemption_count"].(float64)))
	fmt.Fprintf(file, "被中断新诊数,%d\n", int(results["new_patient_preempted"].(float64)))
	fmt.Fprintf(file, "被中断复查数,%d\n", int(results["recheck_patient_preempted"].(float64)))

	return nil
}

// configCmd represents the config command
var configCmd = &cobra.Command{
	Use:   "config",
	Short: "配置管理",
	Long:  `查看和验证仿真配置`,
}

func init() {
	rootCmd.AddCommand(configCmd)
}

// configShowCmd represents the config show subcommand
var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "显示当前配置",
	Long:  `显示当前配置（默认配置或指定配置文件）`,
	Run:   showConfig,
}

func init() {
	configCmd.AddCommand(configShowCmd)
	configShowCmd.Flags().StringP("config", "c", "", "配置文件路径（默认为内置默认配置）")
}

// showConfig displays the current configuration
func showConfig(cmd *cobra.Command, args []string) {
	configPath, _ := cmd.Flags().GetString("config")

	var cfg *configs.Config
	var err error

	if configPath != "" {
		cfg, err = configs.Load(configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "错误: 加载配置失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("配置文件: %s\n", configPath)
	} else {
		cfg = configs.GetDefault()
		fmt.Println("使用内置默认配置")
	}

	// Display configuration
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "\n=== 仿真配置 ===")
	fmt.Fprintln(w, "项目\t值")
	fmt.Fprintln(w, "----\t----")
	fmt.Fprintf(w, "仿真时长\t%d 分钟\n", cfg.Simulation.Duration)
	fmt.Fprintf(w, "重复次数\t%d 次\n", cfg.Simulation.Runs)
	fmt.Fprintln(w, "----\t----")
	fmt.Fprintf(w, "新诊到达率\t%.2f 人/分钟\n", cfg.NewPatient.ArrivalRate)
	fmt.Fprintf(w, "新诊服务时长\t%.1f 分钟\n", cfg.NewPatient.ServiceTime)
	fmt.Fprintln(w, "----\t----")
	fmt.Fprintf(w, "复查到达率\t%.2f 人/分钟\n", cfg.RecheckPatient.ArrivalRate)
	fmt.Fprintf(w, "复查服务时长\t%.1f 分钟\n", cfg.RecheckPatient.ServiceTime)
	fmt.Fprintln(w, "----\t----")
	fmt.Fprintf(w, "Preemption\t%v\n", cfg.Preemption.Enabled)
	if cfg.Preemption.Enabled {
		fmt.Fprintf(w, "Preemption阈值\t%d 分钟\n", cfg.Preemption.ThresholdMinutes)
	}
	fmt.Fprintln(w, "----\t----")
	fmt.Fprintf(w, "输出详细日志\t%v\n", cfg.Output.Verbose)
	fmt.Fprintf(w, "CSV输出路径\t%s\n", cfg.Output.CSVPath)
	fmt.Fprintf(w, "图表输出路径\t%s\n", cfg.Output.ChartPath)
	w.Flush()

	// Validate
	fmt.Println("\n配置验证: 合法")
}

// configValidateCmd represents the config validate subcommand
var configValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "验证配置文件",
	Long:  `验证配置文件是否合法，不合法时输出错误信息`,
	Run:   validateConfig,
}

func init() {
	configCmd.AddCommand(configValidateCmd)
	configValidateCmd.Flags().StringP("config", "c", "", "配置文件路径（必填）")
}

func validateConfig(cmd *cobra.Command, args []string) {
	configPath, _ := cmd.Flags().GetString("config")
	if configPath == "" {
		fmt.Fprintln(os.Stderr, "错误: 必须指定配置文件路径 (--config 或 -c)")
		os.Exit(1)
	}

	cfg, err := configs.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "验证失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("配置文件 %s 验证通过\n", configPath)
	fmt.Println("\n参数范围说明:")
	fmt.Println("- 仿真时长: > 0 分钟")
	fmt.Println("- 到达率: > 0 人/分钟")
	fmt.Println("- 服务时长: > 0 分钟")
	fmt.Println("- Preemption阈值: >= 0 分钟")
	fmt.Printf("\n有效配置:\n")
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "仿真时长: %d 分钟\n", cfg.Simulation.Duration)
	fmt.Fprintf(w, "新诊+复查到达率: %.2f 人/分钟\n", cfg.NewPatient.ArrivalRate+cfg.RecheckPatient.ArrivalRate)
	fmt.Fprintf(w, "Preemption: %v\n", cfg.Preemption.Enabled)
	w.Flush()
}

// configDefaultsCmd represents the config defaults subcommand
var configDefaultsCmd = &cobra.Command{
	Use:   "defaults",
	Short: "显示默认参数范围",
	Long:  `显示各参数的合理取值范围参考`,
	Run:   showDefaults,
}

func init() {
	configCmd.AddCommand(configDefaultsCmd)
}

func showDefaults(cmd *cobra.Command, args []string) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "\n=== 参数默认范围参考 ===")
	fmt.Fprintln(w, "参数\t最小值\t最大值\t说明")
	fmt.Fprintln(w, "----\t----\t----\t----")
	fmt.Fprintln(w, "仿真时长\t60\t1440\t分钟（1小时~24小时）")
	fmt.Fprintln(w, "重复次数\t1\t1000\t每次仿真的独立运行次数")
	fmt.Fprintln(w, "新诊到达率\t0.01\t1.0\t人/分钟")
	fmt.Fprintln(w, "新诊服务时长\t1\t60\t分钟")
	fmt.Fprintln(w, "复查到达率\t0.01\t0.5\t人/分钟")
	fmt.Fprintln(w, "复查服务时长\t1\t30\t分钟")
	fmt.Fprintln(w, "Preemption阈值\t0\t30\t分钟（0表示无限制）")
	fmt.Fprintln(w, "----\t----\t----\t----")
	fmt.Fprintln(w, "\n典型场景示例:")
	fmt.Fprintln(w, "场景\t新诊率\t复查率\t新诊时长\t复查时长")
	fmt.Fprintln(w, "----\t----\t----\t----\t----")
	fmt.Fprintln(w, "普通门诊\t0.1\t0.05\t10\t5")
	fmt.Fprintln(w, "专家门诊\t0.05\t0.1\t15\t8")
	fmt.Fprintln(w, "慢病管理\t0.03\t0.15\t8\t5")
	w.Flush()
}

// historyCmd represents the history command
var historyCmd = &cobra.Command{
	Use:   "history",
	Short: "查询历史仿真结果",
	Long: `从 SQLite 数据库查询历史仿真结果，支持按参数过滤和排序。

支持过滤条件：
  --preemption     只显示启用 preemption 的结果
  --no-preemption  只显示禁用 preemption 的结果
  --limit         限制返回结果数量（默认 20）

使用示例：
  # 查看最近 20 条记录
  sim history

  # 查看最近 50 条记录
  sim history --limit 50

  # 只查看启用 preemption 的记录
  sim history --preemption

  # 只查看禁用 preemption 的记录
  sim history --no-preemption`,
	Run: runHistory,
}

func init() {
	rootCmd.AddCommand(historyCmd)
	historyCmd.Flags().Int("limit", 20, "返回结果数量")
	historyCmd.Flags().Bool("preemption", false, "只显示启用 preemption 的结果")
	historyCmd.Flags().Bool("no-preemption", false, "只显示禁用 preemption 的结果")
}

func runHistory(cmd *cobra.Command, args []string) {
	limit, _ := cmd.Flags().GetInt("limit")
	withPreempt, _ := cmd.Flags().GetBool("preemption")
	withoutPreempt, _ := cmd.Flags().GetBool("no-preemption")

	if sqlite.DB == nil {
		fmt.Fprintln(os.Stderr, "错误: 数据库未初始化")
		os.Exit(1)
	}

	filter := &sqlite.QueryFilter{Limit: limit}
	if withPreempt {
		enabled := true
		filter.PreemptionEnabled = &enabled
	}
	if withoutPreempt {
		enabled := false
		filter.PreemptionEnabled = &enabled
	}

	results, err := sqlite.QueryResults(filter)
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: 查询失败: %v\n", err)
		os.Exit(1)
	}

	if len(results) == 0 {
		fmt.Println("没有找到历史记录")
		return
	}

	// Display results in table format
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "\n=== 历史仿真结果 ===")
	fmt.Fprintf(w, "ID\t时间\tPreemption\t阈值\t平均等待\t新诊等待\t复查等待\t总患者数\n")
	fmt.Fprintln(w, "----\t----\t----\t----\t----\t----\t----\t----")
	for _, r := range results {
		preemptStr := "否"
		if r.PreemptionEnabled {
			preemptStr = "是"
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%.1f\t%.2f\t%.2f\t%.2f\t%d\n",
			r.ID,
			r.Timestamp.Format("2006-01-02 15:04"),
			preemptStr,
			r.PreemptionThreshold,
			r.AvgWaitTime,
			r.NewPatientAvgWait,
			r.RecheckPatientAvgWait,
			r.TotalPatients)
	}
	w.Flush()

	fmt.Printf("\n共 %d 条记录\n", len(results))
}

// generateSampleCmd represents the generate-sample command
var generateSampleCmd = &cobra.Command{
	Use:   "generate-sample",
	Short: "生成示例配置文件",
	Long: `生成符合医院门诊参数范围的示例 YAML 配置文件。

生成的配置文件可用于快速开始仿真，包含经过验证的合理参数范围。

可用场景：
  default      普通门诊（新诊:复查 ≈ 2:1）
  high-recheck 高复查率场景（复查患者为主，如慢病管理）
  low-recheck  低复查率场景（新诊为主，如体检、初诊）
  peak-hours   高峰时段（高流量场景）

使用示例：
  # 生成默认场景配置
  sim generate-sample

  # 生成高峰时段配置
  sim generate-sample -s peak-hours -o configs/peak.yaml

  # 生成高复查率场景
  sim generate-sample -s high-recheck -o configs/my_clinic.yaml

场景参数参考：
  | 场景       | 新诊到达率 | 复查到达率 | 新诊时长 | 复查时长 |
  |------------|-----------|-----------|---------|---------|
  | 普通门诊   | 0.20      | 0.10      | 10分钟   | 5分钟    |
  | 专家门诊   | 0.05      | 0.10      | 15分钟   | 8分钟    |
  | 慢病管理   | 0.03      | 0.15      | 8分钟    | 5分钟    |
  | 高峰时段   | 0.40      | 0.35      | 8分钟    | 5分钟    |`,
	Run:   generateSample,
}

func init() {
	rootCmd.AddCommand(generateSampleCmd)
	generateSampleCmd.Flags().StringP("output", "o", "configs/sample.yaml", "输出文件路径")
	generateSampleCmd.Flags().StringP("scene", "s", "default", "场景类型: default, high-recheck, low-recheck, peak-hours")
}

func generateSample(cmd *cobra.Command, args []string) {
	outputPath, _ := cmd.Flags().GetString("output")
	sceneType, _ := cmd.Flags().GetString("scene")

	var yamlContent string
	switch sceneType {
	case "high-recheck":
		yamlContent = `# 高复查率场景配置（复查患者较多，如慢病管理、复诊）
simulation:
  duration: 480        # 8小时仿真
  runs: 20            # 独立运行次数

new_patient:
  arrival_rate: 0.15   # 新诊到达率 (人/分钟)
  service_time: 12.0   # 新诊服务时长 (分钟)

recheck_patient:
  arrival_rate: 0.25   # 复查到达率较高 (人/分钟)
  service_time: 6.0    # 复查服务时长 (分钟)

preemption:
  enabled: true
  threshold_minutes: 3  # 已服务超过3分钟则允许被高优先级复查中断

output:
  verbose: true
  csv_path: "outputs/sample_high_recheck.csv"
  chart_path: "outputs/sample_high_recheck.png"
`
	case "low-recheck":
		yamlContent = `# 低复查率场景配置（新诊为主，如体检、初诊）
simulation:
  duration: 480
  runs: 20

new_patient:
  arrival_rate: 0.25   # 新诊到达率较高
  service_time: 10.0

recheck_patient:
  arrival_rate: 0.08   # 复查到达率较低
  service_time: 5.0

preemption:
  enabled: true
  threshold_minutes: 3

output:
  verbose: true
  csv_path: "outputs/sample_low_recheck.csv"
  chart_path: "outputs/sample_low_recheck.png"
`
	case "peak-hours":
		yamlContent = `# 高峰时段场景配置（新诊+复查均高，如上午9-11点）
simulation:
  duration: 120        # 2小时高峰时段
  runs: 30            # 高峰期样本量需要更大

new_patient:
  arrival_rate: 0.40   # 高新诊到达率
  service_time: 8.0    # 压缩服务时长以应对高峰

recheck_patient:
  arrival_rate: 0.35   # 高复查到达率
  service_time: 5.0

preemption:
  enabled: true
  threshold_minutes: 2  # 更短阈值，提高 preempt 频率

output:
  verbose: true
  csv_path: "outputs/sample_peak.csv"
  chart_path: "outputs/sample_peak.png"
`
	default:
		yamlContent = `# 默认场景配置（普通门诊，平衡新诊与复查）
simulation:
  duration: 480        # 8小时仿真（一天门诊量）
  runs: 10             # 统计可靠性：10次独立运行

new_patient:
  arrival_rate: 0.20   # 新诊到达率 (人/分钟)
  service_time: 10.0   # 新诊服务时长 (分钟)

recheck_patient:
  arrival_rate: 0.10   # 复查到达率 (人/分钟)
  service_time: 5.0    # 复查服务时长 (分钟)

preemption:
  enabled: true         # 默认启用 preemption
  threshold_minutes: 3  # 已服务超过3分钟则允许被高优先级复查中断

output:
  verbose: false
  csv_path: "outputs/sample.csv"
  chart_path: "outputs/sample_chart.png"
`
	}

	err := os.WriteFile(outputPath, []byte(yamlContent), 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: 写入配置文件失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("示例配置文件已生成: %s\n", outputPath)
	fmt.Printf("场景类型: %s\n", sceneType)
	fmt.Println("\n可用场景:")
	fmt.Println("  default     - 普通门诊（新诊:复查 = 2:1）")
	fmt.Println("  high-recheck - 高复查率（如慢病管理）")
	fmt.Println("  low-recheck  - 低复查率（如体检、初诊）")
	fmt.Println("  peak-hours   - 高峰时段（高流量）")
	fmt.Println("\n示例配置参数说明:")
	fmt.Println("- 仿真时长: 480分钟（8小时门诊）")
	fmt.Println("- 新诊到达率: 0.15-0.40 人/分钟")
	fmt.Println("- 复查到达率: 0.08-0.35 人/分钟")
	fmt.Println("- 新诊服务时长: 8-12 分钟")
	fmt.Println("- 复查服务时长: 5-6 分钟")
	fmt.Println("- Preemption阈值: 2-3 分钟（已服务时长）")
}

// versionCmd represents the version command
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "显示版本号",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("sim version %s\n", version)
	},
}

var rootCmd = &cobra.Command{
	Use:   "sim",
	Short: "复查医嘱抢先中断排队仿真器",
	Long:  `基于 SimPy 的 M/M/1 + preemption 模型，仿真新诊等待 vs 复查插入对门诊平均等待时间的影响`,
}

func init() {
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(executeCmd)
	rootCmd.AddCommand(simulateCmd)
	rootCmd.AddCommand(compareCmd)
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(generateSampleCmd)
	rootCmd.AddCommand(sensitivityCmd)
	rootCmd.AddCommand(historyCmd)
}

func main() {
	// Initialize database on startup
	initDB()

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	// Close database on exit
	sqlite.Close()
}