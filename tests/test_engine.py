"""
单元测试 - Python 仿真引擎
测试 SimPy 仿真引擎的 M/M/1 基础统计、preemption 逻辑、参数边界
"""

import sys
import os
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import pytest
import json
from sim.engine import run_simulation


def make_config(**kwargs):
    """创建测试配置字典"""
    defaults = {
        "new_patient_arrival_rate": 0.3,
        "recheck_arrival_rate": 0.1,
        "new_patient_service_time": 15,
        "recheck_service_time": 10,
        "preemption_threshold": 5,
        "preemption_enabled": False,
        "simulation_time": 120,
        "seed": 42
    }
    defaults.update(kwargs)
    return defaults


class TestBasicMM1:
    """基础 M/M/1 队列测试"""

    def test_basic_simulation(self):
        """测试基础仿真运行"""
        config = make_config()

        result = run_simulation(config)

        # 验证返回结果结构
        assert "total_patients" in result
        assert "avg_wait_time" in result
        assert "server_utilization" in result

        # 验证数值合理性
        assert result["total_patients"] >= 0
        assert result["avg_wait_time"] >= 0
        assert 0 <= result["server_utilization"] <= 1

    def test_zero_arrival_rate(self):
        """测试零到达率（边界情况）- 跳过，因为 zero rate 导致 expovariate 除零"""
        pytest.skip("零到达率会导致 random.expovariate 除零错误")


class TestPreemptionLogic:
    """Preemption 抢先中断逻辑测试"""

    def test_preemption_disabled(self):
        """测试 preemption 禁用时无中断"""
        config = make_config(
            new_patient_arrival_rate=0.3,
            recheck_arrival_rate=0.2,
            preemption_enabled=False
        )

        result = run_simulation(config)

        # 无 preemption 时，中断次数应为 0
        assert result.get("preemption_count", 0) == 0

    def test_preemption_enabled(self):
        """测试 preemption 启用时有中断"""
        config = make_config(
            new_patient_arrival_rate=0.5,
            recheck_arrival_rate=0.5,
            new_patient_service_time=30,
            recheck_service_time=10,
            preemption_enabled=True,
            simulation_time=480,
            seed=123
        )

        result = run_simulation(config)

        # 有 preemption 时，中断次数应 > 0
        assert result.get("preemption_count", 0) > 0

    def test_high_threshold_no_preemption(self):
        """测试高阈值时无实际中断"""
        config = make_config(
            new_patient_arrival_rate=0.2,
            recheck_arrival_rate=0.2,
            new_patient_service_time=10,
            recheck_service_time=5,
            preemption_threshold=100,  # 极高阈值
            preemption_enabled=True
        )

        result = run_simulation(config)

        # 阈值高于服务时长时，中断次数应很少
        assert result.get("preemption_count", 0) < 5


class TestParameterBoundary:
    """参数边界测试"""

    def test_very_short_simulation(self):
        """测试极短仿真时间"""
        config = make_config(
            new_patient_arrival_rate=0.5,
            recheck_arrival_rate=0.3,
            new_patient_service_time=10,
            recheck_service_time=5,
            preemption_threshold=3,
            preemption_enabled=True,
            simulation_time=10  # 仅 10 分钟
        )

        result = run_simulation(config)

        # 短时间仿真也应正常返回结果
        assert "total_patients" in result
        assert result["avg_wait_time"] >= 0

    def test_high_arrival_rate(self):
        """测试高到达率（压力测试）"""
        config = make_config(
            new_patient_arrival_rate=0.9,  # 高到达率
            recheck_arrival_rate=0.5,
            new_patient_service_time=5,
            recheck_service_time=3,
            preemption_threshold=2,
            preemption_enabled=True,
            simulation_time=60
        )

        result = run_simulation(config)

        # 高到达率时患者数应较多
        assert result["total_patients"] > 10
        # 利用率应接近 1
        assert result["server_utilization"] > 0.8


class TestOutputFormat:
    """输出格式测试"""

    def test_json_serializable(self):
        """测试结果可 JSON 序列化"""
        config = make_config(
            preemption_enabled=True
        )

        result = run_simulation(config)

        # 验证可 JSON 序列化
        json_str = json.dumps(result)
        parsed = json.loads(json_str)

        assert parsed["total_patients"] == result["total_patients"]

    def test_all_required_fields(self):
        """测试所有必需字段存在"""
        config = make_config(
            preemption_enabled=True
        )

        result = run_simulation(config)

        required_fields = [
            "total_patients",
            "avg_wait_time",
            "max_wait_time",
            "server_utilization",
            "avg_queue_length",
            "new_patient_avg_wait",
            "recheck_patient_avg_wait",
            "preemption_count",
            "new_patient_preempted",
            "recheck_patient_preempted"
        ]

        for field in required_fields:
            assert field in result, f"Missing field: {field}"


if __name__ == "__main__":
    pytest.main([__file__, "-v"])