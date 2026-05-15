# Outpatient Queue Preemption Simulation

Based on SimPy's M/M/1 + preemption model, simulates the impact of new patient waiting vs. recheck patient insertion on average outpatient waiting time. Golang CLI service layer + Python/SimPy simulation engine, providing quantitative basis for outpatient management decisions on "whether to allow recheck patients to cut in line."

## Core Concepts

- **Queueing Theory Basics**: M/M/1 queue (new patient arrivals follow Poisson process, service time follows exponential distribution, single server)
- **Preemption**: When a recheck patient (priority=1) arrives, if current new patient service time > threshold, the current service is interrupted and recheck patient gets priority
- **Key Metrics**: Average waiting time, queue length, doctor utilization, interruption count

## Installation

### Prerequisites

- Go 1.21+
- Python 3.9+
- pip

### Installation Steps

```bash
# Clone the repository
git clone <repo-url>
cd order-queue-preemption-sim

# Install Python dependencies
pip install -r requirements.txt

# Build Go CLI
go build -o bin/sim ./cmd/sim
```

## Usage

### Generate Sample Configuration

```bash
./bin/sim generate-sample
```

### Single Simulation Run

```bash
./bin/sim simulate --config configs/default.yaml
```

### Sensitivity Analysis

```bash
./bin/sim sensitivity --config configs/default.yaml --output outputs/
```

### View Current Configuration

```bash
./bin/sim config show
```

## Parameter Description

| Parameter | Description | Default |
|-----------|-------------|---------|
| new_patient_rate | New patient arrival rate (people/min) | 0.3 |
| recheck_rate | Recheck patient arrival rate (people/min) | 0.1 |
| service_time | New patient service time (minutes) | 15 |
| preemption_threshold | Preemption threshold (minutes) | 5 |
| simulation_time | Simulation duration (minutes) | 480 |
| num_runs | Number of repetitions | 10 |

## Output Description

- `outputs/sensitivity.csv` - Sensitivity analysis data
- `outputs/sensitivity.png` - Sensitivity analysis charts
- `logs/sim.log` - Runtime logs

## Project Structure

```
order-queue-preemption-sim/
├── cmd/sim/          # Go CLI entry point
├── sim/              # Python simulation engine
├── configs/          # Configuration files
├── data/             # Sample data
├── outputs/          # Output results
└── tests/            # Test code
```

---

## Support the Author

If you find this project helpful, feel free to buy me a coffee! ☕

![Buy Me a Coffee](buymeacoffee.png)

**Buy me a coffee (crypto)**

| Chain | Address |
|-------|---------|
| BTC | `bc1qc0f5tv577z7yt59tw8sqaq3tey98xehy32frzd` |
| ETH / USDT | `0x3b7b6c47491e4778157f0756102f134d05070704` |
| SOL | `6Xuk373zc6x6XWcAAuqvbWW92zabJdCmN3CSwpsVM6sd` |
