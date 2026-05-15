"""
Sensitivity Analysis Visualization
敏感性分析图表生成器，使用 matplotlib 输出敏感性分析图
"""

import sys
import os
# Add project root to path for consistent imports
_project_root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
if _project_root not in sys.path:
    sys.path.insert(0, _project_root)

import matplotlib
matplotlib.use('Agg')  # 使用非交互式后端
import matplotlib.pyplot as plt
import json
import os
from typing import Dict, Any, List, Optional


def load_sensitivity_data(csv_path: str) -> List[Dict[str, Any]]:
    """从 CSV 文件加载敏感性分析数据"""
    import csv

    data = []
    with open(csv_path, 'r', encoding='utf-8') as f:
        reader = csv.DictReader(f)
        for row in reader:
            # 转换数值类型
            numeric_row = {}
            for key, value in row.items():
                try:
                    numeric_row[key] = float(value)
                except (ValueError, TypeError):
                    numeric_row[key] = value
            data.append(numeric_row)
    return data


def plot_sensitivity_analysis(
    data: List[Dict[str, Any]],
    scan_variable: str,
    output_path: str,
    title: str = "敏感性分析结果"
):
    """
    绘制敏感性分析图表

    Args:
        data: 聚合后的敏感性分析数据
        scan_variable: 扫描变量名
        output_path: 输出图片路径
        title: 图表标题
    """
    if not data:
        print(f"No data to plot")
        return

    # 按扫描变量值排序
    data.sort(key=lambda x: x.get(scan_variable, 0))

    # 提取数据
    x_values = [d.get(scan_variable, 0) for d in data]

    # 提取各指标
    avg_wait = [d.get('avg_wait_time', 0) for d in data]
    new_patient_wait = [d.get('new_patient_avg_wait', 0) for d in data]
    recheck_patient_wait = [d.get('recheck_patient_avg_wait', 0) for d in data]
    server_util = [d.get('server_utilization', 0) * 100 for d in data]  # 转为百分比
    preemption_count = [d.get('preemption_count', 0) for d in data]
    avg_queue = [d.get('avg_queue_length', 0) for d in data]

    # 创建图表：2x2 子图布局
    fig, axes = plt.subplots(2, 2, figsize=(14, 10))
    fig.suptitle(title, fontsize=14, fontweight='bold')

    # 子图1：平均等待时间
    ax1 = axes[0, 0]
    ax1.plot(x_values, avg_wait, 'b-o', linewidth=2, markersize=6, label='总体平均等待')
    ax1.plot(x_values, new_patient_wait, 'g--s', linewidth=1.5, markersize=5, label='新诊等待')
    ax1.plot(x_values, recheck_patient_wait, 'r--^', linewidth=1.5, markersize=5, label='复查等待')
    ax1.set_xlabel(scan_variable)
    ax1.set_ylabel('等待时间 (分钟)')
    ax1.set_title('平均等待时间')
    ax1.legend()
    ax1.grid(True, alpha=0.3)

    # 子图2：医生利用率
    ax2 = axes[0, 1]
    ax2.plot(x_values, server_util, 'purple', linewidth=2, marker='D', markersize=6)
    ax2.set_xlabel(scan_variable)
    ax2.set_ylabel('利用率 (%)')
    ax2.set_title('医生利用率')
    ax2.set_ylim(0, 100)
    ax2.grid(True, alpha=0.3)

    # 子图3：中断次数
    ax3 = axes[1, 0]
    ax3.plot(x_values, preemption_count, 'orange', linewidth=2, marker='s', markersize=6)
    ax3.set_xlabel(scan_variable)
    ax3.set_ylabel('中断次数')
    ax3.set_title('Preemption 中断次数')
    ax3.grid(True, alpha=0.3)

    # 子图4：平均队列长度
    ax4 = axes[1, 1]
    ax4.plot(x_values, avg_queue, 'teal', linewidth=2, marker='^', markersize=6)
    ax4.set_xlabel(scan_variable)
    ax4.set_ylabel('队列长度')
    ax4.set_title('平均队列长度')
    ax4.grid(True, alpha=0.3)

    plt.tight_layout()

    # 确保输出目录存在
    os.makedirs(os.path.dirname(output_path) if os.path.dirname(output_path) else '.', exist_ok=True)

    plt.savefig(output_path, dpi=150, bbox_inches='tight')
    plt.close()

    print(f"Chart saved to {output_path}")


def plot_comparison(
    data_no_preempt: List[Dict[str, Any]],
    data_with_preempt: List[Dict[str, Any]],
    scan_variable: str,
    output_path: str,
    title: str = "Preemption 对比分析"
):
    """
    绘制 Preemption 有/无对比图表

    Args:
        data_no_preempt: 无 preemption 的数据
        data_with_preempt: 有 preemption 的数据
        scan_variable: 扫描变量名
        output_path: 输出图片路径
        title: 图表标题
    """
    # 排序
    data_no_preempt.sort(key=lambda x: x.get(scan_variable, 0))
    data_with_preempt.sort(key=lambda x: x.get(scan_variable, 0))

    x_no = [d.get(scan_variable, 0) for d in data_no_preempt]
    x_with = [d.get(scan_variable, 0) for d in data_with_preempt]

    wait_no = [d.get('avg_wait_time', 0) for d in data_no_preempt]
    wait_with = [d.get('avg_wait_time', 0) for d in data_with_preempt]

    new_wait_no = [d.get('new_patient_avg_wait', 0) for d in data_no_preempt]
    new_wait_with = [d.get('new_patient_avg_wait', 0) for d in data_with_preempt]

    # 创建图表
    fig, axes = plt.subplots(1, 2, figsize=(14, 5))
    fig.suptitle(title, fontsize=14, fontweight='bold')

    # 子图1：总体平均等待时间对比
    ax1 = axes[0]
    ax1.plot(x_no, wait_no, 'b-o', linewidth=2, markersize=6, label='无 Preemption')
    ax1.plot(x_with, wait_with, 'r--^', linewidth=2, markersize=6, label='有 Preemption')
    ax1.set_xlabel(scan_variable)
    ax1.set_ylabel('等待时间 (分钟)')
    ax1.set_title('总体平均等待时间对比')
    ax1.legend()
    ax1.grid(True, alpha=0.3)

    # 子图2：新诊患者等待时间对比
    ax2 = axes[1]
    ax2.plot(x_no, new_wait_no, 'g-o', linewidth=2, markersize=6, label='无 Preemption')
    ax2.plot(x_with, new_wait_with, 'm--^', linewidth=2, markersize=6, label='有 Preemption')
    ax2.set_xlabel(scan_variable)
    ax2.set_ylabel('等待时间 (分钟)')
    ax2.set_title('新诊患者平均等待时间对比')
    ax2.legend()
    ax2.grid(True, alpha=0.3)

    plt.tight_layout()

    os.makedirs(os.path.dirname(output_path) if os.path.dirname(output_path) else '.', exist_ok=True)

    plt.savefig(output_path, dpi=150, bbox_inches='tight')
    plt.close()

    print(f"Comparison chart saved to {output_path}")


def main():
    """CLI 入口 - 从 stdin 读取配置，生成图表"""
    try:
        input_data = json.load(__import__('sys').stdin)

        csv_path = input_data.get('csv_path', 'outputs/sensitivity.csv')
        output_path = input_data.get('output_path', 'outputs/sensitivity_chart.png')
        scan_variable = input_data.get('scan_variable', 'preemption_threshold')
        title = input_data.get('title', '敏感性分析结果')

        # 加载数据
        if os.path.exists(csv_path):
            data = load_sensitivity_data(csv_path)
            plot_sensitivity_analysis(data, scan_variable, output_path, title)

            # 如果提供了对比数据，也生成对比图
            if 'csv_path_no_preempt' in input_data and 'csv_path_with_preempt' in input_data:
                data_no = load_sensitivity_data(input_data['csv_path_no_preempt'])
                data_with = load_sensitivity_data(input_data['csv_path_with_preempt'])

                comparison_output = input_data.get('comparison_output', 'outputs/comparison_chart.png')
                plot_comparison(data_no, data_with, scan_variable, comparison_output)
        else:
            print(f"CSV file not found: {csv_path}")

    except Exception as e:
        error_result = {'error': str(e)}
        print(json.dumps(error_result, indent=2, ensure_ascii=False))
        raise


if __name__ == '__main__':
    main()