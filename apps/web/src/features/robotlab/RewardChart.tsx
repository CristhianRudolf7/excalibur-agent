import { useEffect, useRef } from 'react'
import { getInstanceByDom, init, use as registerCharts } from 'echarts/core'
import { LineChart } from 'echarts/charts'
import { GridComponent, LegendComponent, TooltipComponent } from 'echarts/components'
import { SVGRenderer } from 'echarts/renderers'
import type { MetricPoint, Run } from './types'
import { useLabText } from './i18n'
registerCharts([LineChart, GridComponent, LegendComponent, TooltipComponent, SVGRenderer])
export function RewardChart({ points, runs }: { points: MetricPoint[]; runs: Run[] }) {
  const node = useRef<HTMLDivElement>(null)
  const { text } = useLabText()
  useEffect(() => {
    if (!node.current) return
    const chart = init(node.current, undefined, { renderer: 'svg' })
    const observer = new ResizeObserver(() => chart.resize())
    observer.observe(node.current)
    return () => {
      observer.disconnect()
      chart.dispose()
    }
  }, [])
  useEffect(() => {
    if (!node.current) return
    const chart = getInstanceByDom(node.current)
    if (!chart) return
    const style = getComputedStyle(node.current)
    chart.setOption(
      {
        animation: false,
        color: ['--chart-1', '--accent-2', '--violet', '--orange'].map((token) =>
          style.getPropertyValue(token).trim(),
        ),
        textStyle: { color: style.getPropertyValue('--foreground').trim() },
        tooltip: { trigger: 'axis' },
        legend: { data: runs.map((run) => run.label) },
        grid: { left: 55, right: 20, top: 45, bottom: 40 },
        xAxis: { type: 'value', name: text('step') },
        yAxis: { type: 'value', name: text('return'), scale: true },
        series: runs.map((run) => ({
          name: run.label,
          type: 'line',
          showSymbol: false,
          connectNulls: false,
          data: points
            .filter((point) => point.run === run.id && point.episode_return !== undefined)
            .map((point) => [point.step, point.episode_return]),
        })),
      },
      { notMerge: true },
    )
  }, [points, runs, text])
  return (
    <div
      ref={node}
      className="lab-chart"
      data-testid="lab-reward-chart"
      data-points={points.length}
      role="img"
      aria-label={text('return')}
    />
  )
}
