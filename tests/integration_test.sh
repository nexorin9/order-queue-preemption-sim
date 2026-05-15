#!/bin/bash
# 集成测试脚本：测试完整 CLI 流程

set -e

PROJECT_DIR="order-queue-preemption-sim"
cd "$(dirname "$0")/.."

echo "=== Testing generate-sample ==="
./bin/sim.exe generate-sample -s default -o outputs/test_sample.yaml
if [ ! -f "outputs/test_sample.yaml" ]; then
    echo "FAIL: sample file not created"
    exit 1
fi
echo "PASS: generate-sample"

echo ""
echo "=== Testing config validate ==="
./bin/sim.exe config validate -c outputs/test_sample.yaml
echo "PASS: config validate"

echo ""
echo "=== Testing simulate ==="
./bin/sim.exe simulate -c configs/default.yaml -o outputs/test_sim.csv
if [ ! -f "outputs/test_sim.csv" ]; then
    echo "FAIL: simulate csv not created"
    exit 1
fi
echo "PASS: simulate"

echo ""
echo "=== Testing sensitivity run ==="
./bin/sim.exe sensitivity run --config configs/default.yaml --runs-per-value 2
if [ ! -f "outputs/sensitivity.csv" ]; then
    echo "FAIL: sensitivity csv not created"
    exit 1
fi
echo "PASS: sensitivity run"

echo ""
echo "=== Testing compare ==="
./bin/sim.exe compare --runs 2
if [ ! -f "outputs/compare_result.csv" ]; then
    echo "FAIL: compare csv not created"
    exit 1
fi
echo "PASS: compare"

echo ""
echo "=== All integration tests passed ==="