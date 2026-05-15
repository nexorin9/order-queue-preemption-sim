"""
Streamlit Web Visualization Interface for Order Queue Preemption Simulation
门诊排队仿真 Streamlit Web 界面 - 参数配置、实时仿真触发、图表展示
"""

import streamlit as st
import json
import os
import sys
import subprocess
import pandas as pd
import io
from datetime import datetime

# Page configuration
st.set_page_config(
    page_title="门诊排队仿真器",
    page_icon="🏥",
    layout="wide",
    initial_sidebar_state="expanded"
)

# Project root for imports
PROJECT_ROOT = os.path.dirname(os.path.abspath(__file__))
ENGINE_PATH = os.path.join(PROJECT_ROOT, "sim", "engine.py")
RUNNER_PATH = os.path.join(PROJECT_ROOT, "sim", "runner.py")
VISUALIZE_PATH = os.path.join(PROJECT_ROOT, "sim", "visualize.py")
CONFIG_PATH = os.path.join(PROJECT_ROOT, "configs", "default.yaml")
SQLITE_DB_PATH = os.path.join(PROJECT_ROOT, "outputs", "sim_results.db")


def get_python_command():
    """Get Python command based on OS"""
    return "python" if os.name == "nt" else "python3"


def run_simulation(params: dict) -> dict:
    """
    调用 Python 仿真引擎
    输入: 参数字典
    输出: 结果字典
    """
    cmd = [get_python_command(), ENGINE_PATH]
    proc = subprocess.Popen(
        cmd,
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        cwd=PROJECT_ROOT
    )

    json_data = json.dumps(params, ensure_ascii=False)
    stdout, stderr = proc.communicate(input=json_data.encode('utf-8'))

    if proc.returncode != 0:
        error_msg = stderr.decode('utf-8') if stderr else "Unknown error"
        raise RuntimeError(f"Simulation failed: {error_msg}")

    result = json.loads(stdout.decode('utf-8'))
    if 'error' in result:
        raise RuntimeError(result['error'])

    return result


def run_sensitivity(params: dict, scan_variable: str, scan_values: list, runs_per_value: int = 5) -> dict:
    """
    调用 Python runner 进行敏感性分析
    """
    runner_input = {
        "base_params": params,
        "scan_variable": scan_variable,
        "scan_values": scan_values,
        "runs_per_value": runs_per_value,
        "output_csv": os.path.join(PROJECT_ROOT, "outputs", "streamlit_sensitivity.csv"),
        "parallel": True
    }

    cmd = [get_python_command(), RUNNER_PATH]
    proc = subprocess.Popen(
        cmd,
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        cwd=PROJECT_ROOT
    )

    json_data = json.dumps(runner_input, ensure_ascii=False)
    stdout, stderr = proc.communicate(input=json_data.encode('utf-8'))

    if proc.returncode != 0:
        error_msg = stderr.decode('utf-8') if stderr else "Unknown error"
        raise RuntimeError(f"Sensitivity analysis failed: {error_msg}")

    return json.loads(stdout.decode('utf-8'))


def generate_chart(csv_path: str, output_path: str, scan_variable: str, title: str = "敏感性分析"):
    """
    调用 Python visualize 生成图表
    """
    viz_input = {
        "csv_path": csv_path,
        "output_path": output_path,
        "scan_variable": scan_variable,
        "title": title
    }

    cmd = [get_python_command(), VISUALIZE_PATH]
    proc = subprocess.Popen(
        cmd,
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        cwd=PROJECT_ROOT
    )

    json_data = json.dumps(viz_input, ensure_ascii=False)
    stdout, stderr = proc.communicate(input=json_data.encode('utf-8'))

    if proc.returncode != 0:
        error_msg = stderr.decode('utf-8') if stderr else "Chart generation failed"
        raise RuntimeError(error_msg)


def load_history_from_db(db_path: str, limit: int = 20) -> pd.DataFrame:
    """从 SQLite 数据库加载历史记录"""
    try:
        import sqlite3
        if not os.path.exists(db_path):
            return pd.DataFrame()

        conn = sqlite3.connect(db_path)
        query = f"""
            SELECT
                id,
                timestamp,
                preemption_enabled,
                preemption_threshold,
                avg_wait_time,
                new_patient_avg_wait,
                recheck_patient_avg_wait,
                total_patients,
                server_utilization
            FROM simulation_results
            ORDER BY timestamp DESC
            LIMIT {limit}
        """
        df = pd.read_sql_query(query, conn)
        conn.close()
        return df
    except Exception as e:
        st.error(f"加载历史记录失败: {e}")
        return pd.DataFrame()


def format_simulation_params(params: dict) -> str:
    """Format simulation parameters for display"""
    return f"""
**仿真参数:**
- 新诊到达率: {params.get('new_patient_arrival_rate', 0):.2f} 人/分钟
- 复查到达率: {params.get('recheck_arrival_rate', 0):.2f} 人/分钟
- 新诊服务时长: {params.get('new_patient_service_time', 0):.1f} 分钟
- 复查服务时长: {params.get('recheck_service_time', 0):.1f} 分钟
- 仿真时长: {params.get('simulation_time', 0)} 分钟
- Preemption: {'启用' if params.get('preemption_enabled', False) else '禁用'}
{f"- Preemption阈值: {params.get('preemption_threshold', 0):.1f} 分钟" if params.get('preemption_enabled', False) else ""}
"""


# Title and description
st.title("🏥 门诊排队仿真器")
st.markdown("""
**基于 SimPy 的 M/M/1 + Preemption 模型**，仿真新诊等待 vs 复查插入对门诊平均等待时间的影响。
帮助门诊管理者量化理解「复查插队」策略对关键指标的影响。
""")

# Sidebar: Parameter Configuration
st.sidebar.header("⚙️ 仿真参数配置")

# Basic parameters
with st.sidebar.expander("📊 基本参数", expanded=True):
    new_patient_rate = st.slider(
        "新诊到达率 (人/分钟)",
        min_value=0.01,
        max_value=1.0,
        value=0.3,
        step=0.01,
        help="新诊患者平均每分钟到达人数的倒数"
    )
    recheck_rate = st.slider(
        "复查到达率 (人/分钟)",
        min_value=0.01,
        max_value=0.5,
        value=0.1,
        step=0.01,
        help="复查患者平均每分钟到达人数的倒数"
    )
    new_patient_service = st.slider(
        "新诊服务时长 (分钟)",
        min_value=5.0,
        max_value=30.0,
        value=10.0,
        step=0.5,
        help="新诊患者平均服务时间"
    )
    recheck_service = st.slider(
        "复查服务时长 (分钟)",
        min_value=3.0,
        max_value=15.0,
        value=5.0,
        step=0.5,
        help="复查患者平均服务时间"
    )
    simulation_time = st.slider(
        "仿真时长 (分钟)",
        min_value=60,
        max_value=480,
        value=120,
        step=30,
        help="仿真模拟的时间长度（分钟）"
    )

# Preemption parameters
with st.sidebar.expander("🔄 Preemption 设置"):
    preemption_enabled = st.checkbox("启用 Preemption", value=True,
                                      help="允许复查患者打断正在服务的新诊患者")
    preemption_threshold = st.slider(
        "Preemption 阈值 (分钟)",
        min_value=0,
        max_value=30,
        value=3,
        step=1,
        disabled=not preemption_enabled,
        help="只有当新诊患者已接受服务超过此时间，才允许被复查打断"
    )

# Simulation seed
with st.sidebar.expander("🎲 随机种子"):
    seed = st.number_input("随机种子", min_value=0, max_value=999999, value=42, step=1,
                           help="固定种子可复现仿真结果")

# Tabs for different functions
tab1, tab2, tab3 = st.tabs(["📈 单次仿真", "🔍 敏感性分析", "📜 历史记录"])

# Tab 1: Single Simulation
with tab1:
    col1, col2 = st.columns([2, 1])

    with col1:
        st.subheader("运行单次仿真")

        if st.button("▶️ 开始仿真", type="primary", use_container_width=True):
            params = {
                "new_patient_arrival_rate": new_patient_rate,
                "recheck_arrival_rate": recheck_rate,
                "new_patient_service_time": new_patient_service,
                "recheck_service_time": recheck_service,
                "simulation_time": simulation_time,
                "seed": int(seed),
                "preemption_enabled": preemption_enabled,
                "preemption_threshold": float(preemption_threshold)
            }

            with st.spinner("仿真运行中..."):
                try:
                    result = run_simulation(params)

                    # Display results
                    st.success("✅ 仿真完成!")

                    # Key metrics
                    st.markdown("### 📊 仿真结果")
                    metric_cols = st.columns(3)
                    metric_cols[0].metric("平均等待时间", f"{result['avg_wait_time']:.2f} 分钟")
                    metric_cols[1].metric("新诊平均等待", f"{result['new_patient_avg_wait']:.2f} 分钟")
                    metric_cols[2].metric("复查平均等待", f"{result['recheck_patient_avg_wait']:.2f} 分钟")

                    metric_cols2 = st.columns(3)
                    metric_cols2[0].metric("总患者数", f"{int(result['total_patients'])} 人")
                    metric_cols2[1].metric("医生利用率", f"{result['server_utilization']*100:.1f}%")
                    metric_cols2[2].metric("Preemption中断", f"{int(result['preemption_count'])} 次")

                    # Detailed results
                    with st.expander("📋 详细结果"):
                        st.json(result)

                    # Parameters used
                    st.markdown(format_simulation_params(result.get('parameters', params)))

                except Exception as e:
                    st.error(f"❌ 仿真失败: {str(e)}")

    with col2:
        st.subheader("参数说明")
        st.markdown("""
        **到达率**: 指数分布的平均到达间隔的倒数

        **服务时长**: 指数分布的平均服务时间

        **Preemption**: 复查患者可打断正在服务的新诊患者

        **阈值**: 只有当服务时间超过阈值时才允许打断
        """)

# Tab 2: Sensitivity Analysis
with tab2:
    st.subheader("敏感性分析")

    scan_variable = st.selectbox(
        "扫描变量",
        options=[
            ("preemption_threshold", "Preemption 阈值"),
            ("new_patient_arrival_rate", "新诊到达率"),
            ("recheck_arrival_rate", "复查到达率"),
            ("new_patient_service_time", "新诊服务时长")
        ],
        index=0,
        format_func=lambda x: x[1]
    )

    col1, col2 = st.columns(2)
    with col1:
        runs_per_value = st.slider("每个值重复次数", min_value=3, max_value=20, value=10)
    with col2:
        show_chart = st.checkbox("生成图表", value=True)

    if st.button("🔍 开始敏感性分析", type="primary", use_container_width=True):
        params = {
            "new_patient_arrival_rate": new_patient_rate,
            "recheck_arrival_rate": recheck_rate,
            "new_patient_service_time": new_patient_service,
            "recheck_service_time": recheck_service,
            "simulation_time": simulation_time,
            "seed": int(seed),
            "preemption_enabled": preemption_enabled,
            "preemption_threshold": float(preemption_threshold)
        }

        # Determine scan range based on variable
        if scan_variable[0] == "preemption_threshold":
            scan_values = list(range(1, 31))  # 1-30 minutes
            scan_display = "阈值 (1-30 分钟)"
        elif scan_variable[0] == "new_patient_arrival_rate":
            scan_values = [0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.8]
            scan_display = "新诊到达率"
        elif scan_variable[0] == "recheck_arrival_rate":
            scan_values = [0.05, 0.1, 0.15, 0.2, 0.25, 0.3]
            scan_display = "复查到达率"
        else:
            scan_values = [5, 8, 10, 12, 15, 20, 25]
            scan_display = "新诊服务时长"

        with st.spinner(f"正在扫描 {scan_display}..."):
            try:
                result = run_sensitivity(params, scan_variable[0], scan_values, runs_per_value)

                # Display aggregated results
                if "aggregated_results" in result:
                    st.success(f"✅ 敏感性分析完成 ({result.get('successful_runs', 0)}/{result.get('total_runs', 0)} 成功)")

                    df = pd.DataFrame(result["aggregated_results"])
                    df = df.rename(columns={
                        scan_variable[0]: scan_display,
                        "avg_wait_time": "平均等待",
                        "new_patient_avg_wait": "新诊等待",
                        "recheck_patient_avg_wait": "复查等待",
                        "server_utilization": "医生利用率",
                        "preemption_count": "中断次数"
                    })

                    # Line chart for wait time
                    st.markdown(f"### 📈 {scan_display} 对等待时间的影响")
                    chart_data = df[[scan_display, "平均等待", "新诊等待", "复查等待"]].set_index(scan_display)
                    st.line_chart(chart_data)

                    # Show table
                    with st.expander("📋 详细数据"):
                        st.dataframe(df, use_container_width=True)

                    # Find optimal value
                    if scan_variable[0] == "preemption_threshold":
                        best_row = df.loc[df["平均等待"].idxmin()]
                        st.info(f"💡 最优阈值: **{best_row[scan_display]:.0f} 分钟** (平均等待: {best_row['平均等待']:.2f} 分钟)")

                    # Generate chart if requested
                    if show_chart:
                        csv_path = os.path.join(PROJECT_ROOT, "outputs", "streamlit_sensitivity.csv")
                        chart_path = os.path.join(PROJECT_ROOT, "outputs", "streamlit_sensitivity_chart.png")
                        try:
                            generate_chart(csv_path, chart_path, scan_variable[0], "敏感性分析")
                            st.image(chart_path, caption="敏感性分析图表")
                        except Exception as e:
                            st.warning(f"图表生成失败: {e}")

            except Exception as e:
                st.error(f"❌ 敏感性分析失败: {str(e)}")

# Tab 3: History
with tab3:
    st.subheader("历史仿真记录")

    limit = st.slider("显示记录数", min_value=5, max_value=100, value=20)

    if os.path.exists(SQLITE_DB_PATH):
        df = load_history_from_db(SQLITE_DB_PATH, limit)

        if not df.empty:
            # Format the dataframe
            df['timestamp'] = pd.to_datetime(df['timestamp']).dt.strftime('%Y-%m-%d %H:%M')
            df['preemption'] = df['preemption_enabled'].apply(lambda x: '是' if x else '否')
            df['医生利用率'] = (df['server_utilization'] * 100).round(1).astype(str) + '%'

            # Display
            st.dataframe(
                df[['timestamp', 'preemption', 'preemption_threshold', 'avg_wait_time',
                    'new_patient_avg_wait', 'recheck_patient_avg_wait', 'total_patients', '医生利用率']],
                column_config={
                    "timestamp": "时间",
                    "preemption": "Preemption",
                    "preemption_threshold": st.column_config.NumberColumn("阈值(分钟)", format="%.0f"),
                    "avg_wait_time": st.column_config.NumberColumn("平均等待", format="%.2f"),
                    "new_patient_avg_wait": st.column_config.NumberColumn("新诊等待", format="%.2f"),
                    "recheck_patient_avg_wait": st.column_config.NumberColumn("复查等待", format="%.2f"),
                    "total_patients": st.column_config.NumberColumn("总患者", format="%d"),
                    "医生利用率": "利用率"
                },
                use_container_width=True,
                hide_index=True
            )

            st.caption(f"共 {len(df)} 条记录")
        else:
            st.info("暂无历史记录，请先运行几次仿真")
    else:
        st.info(f"数据库不存在 ({SQLITE_DB_PATH})，请先运行 `sim simulate` 命令")

# Footer
st.markdown("---")
st.markdown(
    "🏥 **门诊排队仿真器** - 基于 SimPy M/M/1 + Preemption 模型 | "
    "帮助门诊管理者量化理解「复查插队」策略的影响"
)