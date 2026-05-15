# 复查医嘱抢先中断排队仿真器

基于 SimPy 的 M/M/1 + preemption 模型，仿真新诊等待 vs 复查插入对门诊平均等待时间的影响。Golang CLI 服务层 + Python/SimPy 仿真引擎，为门诊管理提供「是否允许复查插队」的量化依据。

## 核心概念

- **排队论基础**：M/M/1 队列（新诊到达遵循泊松过程，服务时长遵循指数分布，单一服务台）
- **Preemption 抢先中断**：当复查患者（优先级=1）到达时，若当前新诊服务时间 > 阈值，中断当前服务，复查优先就诊
- **关键指标**：平均等待时间、队列长度、医生利用率、中断次数

## 安装

### 前置依赖

- Go 1.21+
- Python 3.9+
- pip

### 安装步骤

```bash
# 克隆仓库
git clone <repo-url>
cd order-queue-preemption-sim

# 安装 Python 依赖
pip install -r requirements.txt

# 构建 Go CLI
go build -o bin/sim ./cmd/sim
```

## 使用方法

### 生成示例配置

```bash
./bin/sim generate-sample
```

### 单次仿真

```bash
./bin/sim simulate --config configs/default.yaml
```

### 敏感性分析

```bash
./bin/sim sensitivity --config configs/default.yaml --output outputs/
```

### 查看当前配置

```bash
./bin/sim config show
```

## 参数说明

| 参数 | 说明 | 默认值 |
|------|------|--------|
| new_patient_rate | 新诊到达率（人/分钟） | 0.3 |
| recheck_rate | 复查到达率（人/分钟） | 0.1 |
| service_time | 新诊服务时长（分钟） | 15 |
| preemption_threshold | 抢先中断阈值（分钟） | 5 |
| simulation_time | 仿真时长（分钟） | 480 |
| num_runs | 重复次数 | 10 |

## 输出说明

- `outputs/sensitivity.csv` - 敏感性分析数据
- `outputs/sensitivity.png` - 敏感性分析图表
- `logs/sim.log` - 运行日志

## 项目结构

```
order-queue-preemption-sim/
├── cmd/sim/          # Go CLI 入口
├── sim/              # Python 仿真引擎
├── configs/          # 配置文件
├── data/             # 示例数据
├── outputs/          # 输出结果
└── tests/            # 测试代码
```

---

## 支持作者

如果您觉得这个项目对您有帮助，欢迎打赏支持！
Wechat:gdgdmp
![Buy Me a Coffee](buymeacoffee.png)

**Buy me a coffee (crypto)**

| 币种 | 地址 |
|------|------|
| BTC | `bc1qc0f5tv577z7yt59tw8sqaq3tey98xehy32frzd` |
| ETH / USDT | `0x3b7b6c47491e4778157f0756102f134d05070704` |
| SOL | `6Xuk373zc6x6XWcAAuqvbWW92zabJdCmN3CSwpsVM6sd` |