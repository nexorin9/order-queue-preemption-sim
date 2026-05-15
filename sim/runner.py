"""
Multi-scenario Parameterized Runner for Queue Simulation
多场景参数化运行器，支持参数扫描和敏感性分析
"""

import json
import csv
import concurrent.futures
from typing import Dict, Any, List, Optional
from dataclasses import dataclass
from datetime import datetime
import os
import sys

# Add the project root to sys.path for both module and script mode
_script_dir = os.path.dirname(os.path.abspath(__file__))
_project_root = os.path.dirname(_script_dir)
if _project_root not in sys.path:
    sys.path.insert(0, _project_root)

# Import after path setup
from sim.engine import run_simulation, SimulationParams


@dataclass
class ScanConfig:
    """参数扫描配置"""
    scan_variable: str  # 要扫描的变量名
    scan_values: List[float]  # 扫描值列表
    base_params: Dict[str, Any]  # 基础参数


@dataclass
class ScanResult:
    """扫描结果"""
    params: Dict[str, Any]
    stats: Dict[str, Any]


def run_single_simulation(params: Dict[str, Any]) -> Dict[str, Any]:
    """运行单次仿真，返回统计结果"""
    try:
        result = run_simulation(params)
        # Check if the result contains an error
        if 'error' in result:
            return {
                'success': False,
                'params': params,
                'error': result['error']
            }
        return {
            'success': True,
            'params': result.get('parameters', params),
            'stats': result
        }
    except ValueError as e:
        # Parameter validation errors from engine.py
        return {
            'success': False,
            'params': params,
            'error': f"参数错误: {str(e)}"
        }
    except Exception as e:
        return {
            'success': False,
            'params': params,
            'error': f"仿真执行失败: {str(e)}"
        }


def run_parameter_scan(
    base_params: Dict[str, Any],
    scan_variable: str,
    scan_values: List[float],
    runs_per_value: int = 10,
    parallel: bool = True
) -> List[ScanResult]:
    """
    参数扫描主函数

    Args:
        base_params: 基础参数字典
        scan_variable: 要扫描的变量名（如 'preemption_threshold'）
        scan_values: 扫描值列表
        runs_per_value: 每个扫描值运行的次数（用于求平均）
        parallel: 是否并行运行

    Returns:
        扫描结果列表
    """
    results = []

    # 构建所有参数组合
    all_params = []
    for value in scan_values:
        for run in range(runs_per_value):
            params = base_params.copy()
            params[scan_variable] = value
            params['seed'] = 42 + run  # 不同随机种子
            all_params.append((value, params))

    # 并行或串行运行
    if parallel:
        with concurrent.futures.ProcessPoolExecutor(max_workers=4) as executor:
            futures = {executor.submit(run_single_simulation, p): (v, p) for v, p in all_params}
            for future in concurrent.futures.as_completed(futures):
                result = future.result()
                results.append(result)
    else:
        for _, params in all_params:
            result = run_single_simulation(params)
            results.append(result)

    return results


def aggregate_results(results: List[Dict[str, Any]], scan_variable: str) -> List[Dict[str, Any]]:
    """
    聚合多次运行的结果

    Args:
        results: 所有单次运行的结果
        scan_variable: 扫描变量名

    Returns:
        按扫描值分组的聚合结果
    """
    # 按扫描值分组
    grouped = {}
    for r in results:
        if not r.get('success'):
            continue
        params = r.get('params', {})
        value = params.get(scan_variable)
        if value not in grouped:
            grouped[value] = []
        grouped[value].append(r.get('stats', {}))

    # 计算聚合统计
    aggregated = []
    for value in sorted(grouped.keys()):
        runs = grouped[value]
        if not runs:
            continue

        # 计算各指标的平均值
        avg_wait = sum(r.get('avg_wait_time', 0) for r in runs) / len(runs)
        avg_queue = sum(r.get('avg_queue_length', 0) for r in runs) / len(runs)
        avg_util = sum(r.get('server_utilization', 0) for r in runs) / len(runs)
        avg_preempt = sum(r.get('preemption_count', 0) for r in runs) / len(runs)

        # 新诊/复查分开统计
        new_avg_wait = sum(r.get('new_patient_avg_wait', 0) for r in runs) / len(runs)
        recheck_avg_wait = sum(r.get('recheck_patient_avg_wait', 0) for r in runs) / len(runs)

        aggregated.append({
            scan_variable: value,
            'total_patients': sum(r.get('total_patients', 0) for r in runs) / len(runs),
            'avg_wait_time': avg_wait,
            'max_wait_time': max(r.get('max_wait_time', 0) for r in runs),
            'avg_queue_length': avg_queue,
            'max_queue_length': max(r.get('max_queue_length', 0) for r in runs),
            'server_utilization': avg_util,
            'preemption_count': avg_preempt,
            'new_patient_avg_wait': new_avg_wait,
            'recheck_patient_avg_wait': recheck_avg_wait,
            'runs': len(runs)
        })

    return aggregated


def write_csv_results(aggregated: List[Dict[str, Any]], output_path: str):
    """
    将聚合结果写入 CSV 文件
    """
    if not aggregated:
        return

    fieldnames = [
        'preemption_threshold' if 'preemption_threshold' in aggregated[0] else list(aggregated[0].keys())[0],
        'total_patients',
        'avg_wait_time',
        'max_wait_time',
        'avg_queue_length',
        'max_queue_length',
        'server_utilization',
        'preemption_count',
        'new_patient_avg_wait',
        'recheck_patient_avg_wait',
        'runs'
    ]

    # 动态获取实际的扫描变量名
    actual_variable = None
    for key in aggregated[0].keys():
        if key not in fieldnames and key != 'runs':
            actual_variable = key
            break

    if actual_variable and actual_variable not in fieldnames:
        fieldnames.insert(0, actual_variable)

    os.makedirs(os.path.dirname(output_path) if os.path.dirname(output_path) else '.', exist_ok=True)

    with open(output_path, 'w', newline='', encoding='utf-8') as f:
        writer = csv.DictWriter(f, fieldnames=fieldnames)
        writer.writeheader()
        for row in aggregated:
            writer.writerow(row)


def run_sensitivity_analysis(
    base_params: Dict[str, Any],
    scan_variable: str,
    scan_values: List[float],
    runs_per_value: int = 10,
    output_csv: Optional[str] = None
) -> Dict[str, Any]:
    """
    敏感性分析主函数

    Args:
        base_params: 基础参数
        scan_variable: 扫描变量
        scan_values: 扫描值列表
        runs_per_value: 每个值的重复次数
        output_csv: CSV 输出路径（可选）

    Returns:
        分析结果字典
    """
    import sys
    print(f"Starting sensitivity analysis: {scan_variable}", file=sys.stderr)
    print(f"Scan values: {scan_values}", file=sys.stderr)
    print(f"Runs per value: {runs_per_value}", file=sys.stderr)

    # 运行参数扫描
    results = run_parameter_scan(base_params, scan_variable, scan_values, runs_per_value)

    # 统计成功/失败
    success_count = sum(1 for r in results if r.get('success'))
    print(f"Completed {success_count}/{len(results)} simulations successfully", file=sys.stderr)

    # 聚合结果
    aggregated = aggregate_results(results, scan_variable)

    # 写入 CSV
    if output_csv:
        write_csv_results(aggregated, output_csv)
        print(f"Results written to {output_csv}", file=sys.stderr)

    return {
        'scan_variable': scan_variable,
        'scan_values': scan_values,
        'runs_per_value': runs_per_value,
        'total_runs': len(results),
        'successful_runs': success_count,
        'aggregated_results': aggregated,
        'csv_output': output_csv
    }


def main():
    """CLI 入口 - 从 stdin 读取配置，输出 JSON 结果"""
    try:
        input_data = json.load(__import__('sys').stdin)

        # Validate input is a dictionary
        if not isinstance(input_data, dict):
            raise ValueError(f"输入必须是 JSON 对象，实际: {type(input_data)}")

        base_params = input_data.get('base_params', {})
        scan_variable = input_data.get('scan_variable', 'preemption_threshold')
        scan_values = input_data.get('scan_values', list(range(0, 31, 5)))
        runs_per_value = input_data.get('runs_per_value', 10)
        output_csv = input_data.get('output_csv', 'outputs/sensitivity.csv')

        result = run_sensitivity_analysis(
            base_params, scan_variable, scan_values,
            runs_per_value, output_csv
        )

        print(json.dumps(result, indent=2, ensure_ascii=False))

    except json.JSONDecodeError as e:
        error_result = {'error': f'JSON 解析失败: {str(e)}'}
        print(json.dumps(error_result, indent=2, ensure_ascii=False))
        sys.exit(1)
    except ValueError as e:
        error_result = {'error': f'参数错误: {str(e)}'}
        print(json.dumps(error_result, indent=2, ensure_ascii=False))
        sys.exit(1)
    except Exception as e:
        error_result = {'error': f'Runner 执行失败: {str(e)}'}
        print(json.dumps(error_result, indent=2, ensure_ascii=False))
        sys.exit(1)


if __name__ == '__main__':
    main()