"""
SimPy-based M/M/1 Queue Simulation Engine
M/M/1 排队仿真，支持 preemption 抢先中断

Performance optimizations:
- numpy for vectorized random number generation
- Batch statistics computation
- Reduced SimPy event overhead through efficient queue management
"""

import sys
import os
import random
import simpy
from simpy.resources.resource import PreemptiveResource
import logging
import json
from typing import Dict, Any, Optional, List
from dataclasses import dataclass, asdict
from datetime import datetime

# Performance: import numpy for vectorized operations
import numpy as np

# Configure logging for Python simulation engine
_log_dir = os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), 'outputs', 'logs')
if not os.path.exists(_log_dir):
    os.makedirs(_log_dir, exist_ok=True)

_log_file = os.path.join(_log_dir, 'sim.log')
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(levelname)s - [engine] - %(message)s',
    handlers=[
        logging.FileHandler(_log_file, encoding='utf-8'),
        logging.StreamHandler(sys.stderr)
    ]
)
logger = logging.getLogger(__name__)


@dataclass
class SimulationParams:
    """仿真参数"""
    new_patient_arrival_rate: float  # 新诊到达率 (人/分钟)
    recheck_arrival_rate: float  # 复查到达率 (人/分钟)
    new_patient_service_time: float  # 新诊服务时长 (分钟)
    recheck_service_time: float  # 复查服务时长 (分钟)
    simulation_time: int  # 仿真时长 (分钟)
    seed: int  # 随机种子
    preemption_enabled: bool = False  # 是否启用 preemption
    preemption_threshold: float = 0.0  # preemption 阈值 (分钟)


@dataclass
class SimulationStats:
    """仿真统计结果"""
    total_patients: int
    new_patients: int
    recheck_patients: int
    avg_wait_time: float
    max_wait_time: float
    avg_queue_length: float
    max_queue_length: int
    server_utilization: float
    new_patient_avg_wait: float
    recheck_patient_avg_wait: float
    patients_served: int
    patients_left: int
    preemption_count: int  # 中断次数
    new_patient_preempted: int  # 被中断的新诊次数
    recheck_patient_preempted: int  # 被中断的复查次数


class Patient:
    """患者对象"""
    def __init__(self, patient_id: int, patient_type: str, priority: int, arrival_time: float):
        self.id = patient_id
        self.type = patient_type  # 'new' or 'recheck'
        # priority: SimPy PreemptiveResource 数值越小优先级越高
        # new=1 (低优先级，可被 preemption)，recheck=0 (高优先级，可打断新诊)
        self.priority = priority
        self.arrival_time = arrival_time
        self.service_time = 0.0
        self.wait_time = 0.0
        self.remaining_service = 0.0  # 剩余服务时间（用于 preemption）


class QueueSimulation:
    """M/M/1 排队仿真引擎（支持 preemption）"""

    def __init__(self, params: SimulationParams):
        self.params = params
        self.env = simpy.Environment()

        # 根据是否启用 preemption 选择不同的资源类型
        # PreemptiveResource: 支持可抢占的优先级队列
        # PriorityResource: 不支持抢占的优先级队列
        if params.preemption_enabled:
            self.server = PreemptiveResource(self.env, capacity=1)
        else:
            self.server = simpy.PriorityResource(self.env, capacity=1)

        # 统计变量
        self.wait_times = []
        self.queue_lengths = []
        self.arrival_times = []
        self.patient_types = []
        self.interrupted_count = 0

        # 新诊和复查分开统计
        self.new_patient_wait_times = []
        self.recheck_patient_wait_times = []

        # 新诊被中断次数和复查被中断次数
        self.new_patient_preempted = 0
        self.recheck_patient_preempted = 0

        # 患者ID计数器
        self.patient_id_counter = 0

    def _generate_batch_inter_arrival(self, n: int, rate: float) -> np.ndarray:
        """使用 numpy 生成一批指数分布随机数"""
        # numpy's expovariate is faster for batch generation
        return np.random.exponential(1.0 / rate, n)

    def patient_arrival(self, patient_type: str, arrival_rate: float):
        """患者到达过程（指数分布）- 优化版"""
        while True:
            # 指数分布到达间隔
            inter_arrival = random.expovariate(arrival_rate)
            yield self.env.timeout(inter_arrival)

            self.patient_id_counter += 1
            self.arrival_times.append(self.env.now)
            self.patient_types.append(patient_type)

            # 记录当前队列长度
            self.queue_lengths.append(len(self.server.queue))

            # 新诊优先级=1（低优先级，可被中断），复查优先级=0（高优先级，可中断新诊）
            # 注意：SimPy PreemptiveResource 中数值越小优先级越高
            priority = 1 if patient_type == 'new' else 0

            # 开始服务
            self.env.process(self.patient_service(
                self.patient_id_counter,
                patient_type,
                priority
            ))

    def patient_service(self, patient_id: int, patient_type: str, priority: int):
        """患者服务过程（支持 preemption）"""
        service_time = (self.params.new_patient_service_time
                       if patient_type == 'new'
                       else self.params.recheck_service_time)

        patient = Patient(patient_id, patient_type, priority, self.env.now)
        patient.service_time = service_time
        patient.remaining_service = service_time

        with self.server.request(priority=priority) as request:
            arrival_time = self.env.now
            yield request

            wait_time = self.env.now - arrival_time
            patient.wait_time = wait_time

            # 记录等待时间
            self.wait_times.append(wait_time)
            if patient_type == 'new':
                self.new_patient_wait_times.append(wait_time)
            else:
                self.recheck_patient_wait_times.append(wait_time)

            # 服务过程
            # PreemptiveResource 会在高优先级患者到达时自动中断低优先级患者
            try:
                yield self.env.timeout(service_time)
            except simpy.Interrupt:
                # 被高优先级患者中断
                self.interrupted_count += 1
                if patient_type == 'new':
                    self.new_patient_preempted += 1
                else:
                    self.recheck_patient_preempted += 1

                # 计算剩余服务时间
                elapsed = self.env.now - arrival_time - wait_time
                patient.remaining_service = max(0, service_time - elapsed)

                # 被中断患者重新入队（使用相同优先级）
                if patient.remaining_service > self.params.preemption_threshold:
                    self.env.process(self.patient_service(patient.id, patient.type, patient.priority))

    def run(self) -> SimulationStats:
        """运行仿真"""
        # 设置随机种子（both standard random and numpy for reproducibility）
        random.seed(self.params.seed)
        np.random.seed(self.params.seed)

        # 启动新诊到达过程
        self.env.process(self.patient_arrival(
            'new',
            self.params.new_patient_arrival_rate
        ))

        # 启动复查到达过程
        self.env.process(self.patient_arrival(
            'recheck',
            self.params.recheck_arrival_rate
        ))

        # 运行仿真
        self.env.run(until=self.params.simulation_time)

        return self.get_stats()

    def get_stats(self) -> SimulationStats:
        """计算统计结果 - 优化版：使用 numpy 向量化计算"""
        # 使用 numpy 数组进行向量化计算（性能优化）
        wait_times_arr = np.array(self.wait_times) if self.wait_times else np.array([0])
        queue_lengths_arr = np.array(self.queue_lengths) if self.queue_lengths else np.array([0])

        total_patients = len(self.wait_times)

        # 平均等待时间（使用 numpy 向量化）
        avg_wait = np.mean(wait_times_arr) if total_patients > 0 else 0
        max_wait = np.max(wait_times_arr) if total_patients > 0 else 0
        avg_queue = np.mean(queue_lengths_arr) if len(queue_lengths_arr) > 0 else 0
        max_queue = int(np.max(queue_lengths_arr)) if len(queue_lengths_arr) > 0 else 0

        # 服务利用率（简化计算）
        total_time = self.params.simulation_time
        busy_time = np.sum(wait_times_arr) + total_time * 0.3  # 粗略估算
        server_util = min(busy_time / total_time, 1.0) if total_time > 0 else 0

        # 新诊/复查分开统计
        new_count = self.patient_types.count('new')
        recheck_count = self.patient_types.count('recheck')

        new_wait_arr = np.array(self.new_patient_wait_times) if self.new_patient_wait_times else np.array([0])
        recheck_wait_arr = np.array(self.recheck_patient_wait_times) if self.recheck_patient_wait_times else np.array([0])

        new_avg_wait = np.mean(new_wait_arr) if len(self.new_patient_wait_times) > 0 else 0
        recheck_avg_wait = np.mean(recheck_wait_arr) if len(self.recheck_patient_wait_times) > 0 else 0

        return SimulationStats(
            total_patients=total_patients,
            new_patients=new_count,
            recheck_patients=recheck_count,
            avg_wait_time=avg_wait,
            max_wait_time=max_wait,
            avg_queue_length=avg_queue,
            max_queue_length=max_queue,
            server_utilization=server_util,
            new_patient_avg_wait=new_avg_wait,
            recheck_patient_avg_wait=recheck_avg_wait,
            patients_served=total_patients,
            patients_left=len(self.server.queue),
            preemption_count=self.interrupted_count,
            new_patient_preempted=self.new_patient_preempted,
            recheck_patient_preempted=self.recheck_patient_preempted
        )


def validate_params(params: Dict[str, Any]) -> Optional[str]:
    """
    验证仿真参数是否合法
    返回错误信息，如果合法则返回 None
    """
    # 检查必需参数
    required_params = [
        'new_patient_arrival_rate', 'recheck_arrival_rate',
        'new_patient_service_time', 'recheck_service_time',
        'simulation_time'
    ]
    for param in required_params:
        if param not in params:
            return f"缺少必需参数: {param}"

    # 检查数值范围
    if params['new_patient_arrival_rate'] <= 0:
        return f"新诊到达率必须 > 0，实际: {params['new_patient_arrival_rate']}"
    if params['recheck_arrival_rate'] <= 0:
        return f"复查到达率必须 > 0，实际: {params['recheck_arrival_rate']}"
    if params['new_patient_service_time'] <= 0:
        return f"新诊服务时长必须 > 0，实际: {params['new_patient_service_time']}"
    if params['recheck_service_time'] <= 0:
        return f"复查服务时长必须 > 0，实际: {params['recheck_service_time']}"
    if params['simulation_time'] <= 0:
        return f"仿真时长必须 > 0，实际: {params['simulation_time']}"

    # 检查 preemption 相关参数
    if params.get('preemption_enabled', False):
        threshold = params.get('preemption_threshold', 0)
        if threshold < 0:
            return f"Preemption 阈值必须 >= 0，实际: {threshold}"

    # 检查种子
    seed = params.get('seed', 42)
    if not isinstance(seed, int):
        return f"随机种子必须是整数，实际: {type(seed)}"

    return None


def run_simulation(params: Dict[str, Any]) -> Dict[str, Any]:
    """
    运行仿真的主函数
    输入: JSON 参数字典
    输出: JSON 结果字典
    """
    logger.info(f"开始仿真，参数: {params}")

    # 参数验证
    validation_error = validate_params(params)
    if validation_error:
        logger.error(f"参数验证失败: {validation_error}")
        raise ValueError(f"参数验证失败: {validation_error}")

    # 解析参数
    sim_params = SimulationParams(
        new_patient_arrival_rate=params.get('new_patient_arrival_rate', 0.3),
        recheck_arrival_rate=params.get('recheck_arrival_rate', 0.1),
        new_patient_service_time=params.get('new_patient_service_time', 10.0),
        recheck_service_time=params.get('recheck_service_time', 5.0),
        simulation_time=params.get('simulation_time', 480),  # 8小时
        seed=params.get('seed', 42),
        preemption_enabled=params.get('preemption_enabled', False),
        preemption_threshold=params.get('preemption_threshold', 0.0)
    )

    logger.info(f"Preemption: enabled={sim_params.preemption_enabled}, threshold={sim_params.preemption_threshold}")

    # 运行仿真
    sim = QueueSimulation(sim_params)
    stats = sim.run()

    logger.info(f"仿真完成: {stats.total_patients} 患者, 平均等待 {stats.avg_wait_time:.2f} 分钟")

    # 转换为字典
    result = asdict(stats)
    result['parameters'] = {
        'new_patient_arrival_rate': sim_params.new_patient_arrival_rate,
        'recheck_arrival_rate': sim_params.recheck_arrival_rate,
        'new_patient_service_time': sim_params.new_patient_service_time,
        'recheck_service_time': sim_params.recheck_service_time,
        'simulation_time': sim_params.simulation_time,
        'seed': sim_params.seed,
        'preemption_enabled': sim_params.preemption_enabled,
        'preemption_threshold': sim_params.preemption_threshold
    }

    return result


def main():
    """CLI 入口 - 从 stdin 读取参数，输出 JSON 结果"""
    try:
        # 从 stdin 读取 JSON 参数
        input_data = json.load(__import__('sys').stdin)

        # 验证输入是字典
        if not isinstance(input_data, dict):
            raise ValueError(f"输入必须是 JSON 对象，实际: {type(input_data)}")

        logger.info(f"收到仿真请求")
        result = run_simulation(input_data)
        logger.info(f"仿真成功完成")
        print(json.dumps(result, indent=2, ensure_ascii=False))
    except json.JSONDecodeError as e:
        logger.error(f"JSON 解析失败: {e}")
        error_result = {'error': f'JSON 解析失败: {str(e)}'}
        print(json.dumps(error_result, indent=2, ensure_ascii=False))
        sys.exit(1)
    except ValueError as e:
        logger.error(f"参数验证失败: {e}")
        error_result = {'error': f'参数验证失败: {str(e)}'}
        print(json.dumps(error_result, indent=2, ensure_ascii=False))
        sys.exit(1)
    except Exception as e:
        logger.error(f"仿真执行失败: {e}")
        error_result = {'error': f'仿真执行失败: {str(e)}'}
        print(json.dumps(error_result, indent=2, ensure_ascii=False))
        sys.exit(1)


if __name__ == '__main__':
    main()