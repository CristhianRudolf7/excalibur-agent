import { useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { init, use as registerCharts } from 'echarts/core'
import { BarChart, PieChart } from 'echarts/charts'
import { GridComponent, LegendComponent, TooltipComponent } from 'echarts/components'
import { SVGRenderer } from 'echarts/renderers'
import type { UsageSummary } from '@/lib/rpc/contract'
registerCharts([BarChart, PieChart, GridComponent, LegendComponent, TooltipComponent, SVGRenderer])
export default function UsageChart({
  buckets,
  models,
}: {
  buckets?: UsageSummary['by_bucket']
  models?: UsageSummary['by_model']
}) {
  const ref = useRef<HTMLDivElement>(null)
  const { t, i18n } = useTranslation()
  useEffect(() => {
    if (!ref.current) return
    const chart = init(ref.current, undefined, { renderer: 'svg' })
    const motion = matchMedia('(prefers-reduced-motion: reduce)')
    let rendered = false
    const render = () => {
      const style = getComputedStyle(document.documentElement)
      const token = (name: string) => style.getPropertyValue(name).trim()
      chart.setOption(
        {
          animation: !motion.matches && !rendered,
          animationDuration: 420,
          animationDurationUpdate: 220,
          color: [1, 2, 3, 4, 5].map((index) => token(`--chart-${index}`)),
          textStyle: {
            color: token('--muted-foreground'),
            fontFamily: 'Inter Variable, sans-serif',
          },
          tooltip: {
            trigger: models ? 'item' : 'axis',
            renderMode: 'richText',
            backgroundColor: token('--surface'),
            textStyle: { color: token('--foreground') },
            borderColor: token('--border'),
          },
          ...(models
            ? {
                series: [
                  {
                    type: 'pie',
                    radius: ['72%', '84%'],
                    center: ['50%', '50%'],
                    label: { show: false },
                    itemStyle: { borderColor: token('--surface'), borderWidth: 3 },
                    data: models.map((row) => ({
                      name: row.model,
                      value: row.input_tokens + row.output_tokens,
                    })),
                  },
                ],
              }
            : {
                grid: { left: 50, right: 12, top: 48, bottom: 40 },
                legend: {
                  left: 0,
                  top: 0,
                  itemWidth: 8,
                  itemHeight: 8,
                  textStyle: { color: token('--muted-foreground'), fontSize: 10 },
                },
                xAxis: {
                  type: 'category',
                  data: buckets?.map((bucket) =>
                    new Date(bucket.start).toLocaleString(i18n.language, {
                      month: 'short',
                      day: 'numeric',
                      ...(buckets.length > 1 &&
                      new Date(buckets[1]!.start).getTime() -
                        new Date(buckets[0]!.start).getTime() <
                        86400_000
                        ? { hour: '2-digit' as const }
                        : {}),
                    }),
                  ),
                  axisLabel: { color: token('--muted-foreground'), fontSize: 10, margin: 12 },
                  axisTick: { show: false },
                  axisLine: { lineStyle: { color: token('--border') } },
                },
                yAxis: {
                  type: 'value',
                  axisLabel: { color: token('--muted-foreground'), fontSize: 10 },
                  splitLine: { lineStyle: { color: token('--border'), type: 'dashed' } },
                },
                series: (['input_tokens', 'cached_tokens', 'output_tokens'] as const).map(
                  (key, index) => ({
                    name: t(['settingsInput', 'settingsCached', 'settingsOutput'][index]!),
                    type: 'bar',
                    itemStyle: { color: token(['--chart-1', '--chart-4', '--chart-2'][index]!) },
                    animationDelay: Math.min(index, 2) * 10,
                    stack: 'tokens',
                    barMaxWidth: 14,
                    data: buckets?.map((bucket) => bucket[key]),
                  }),
                ),
              }),
        },
        true,
      )
      rendered = true
    }
    render()
    const theme = new MutationObserver(render)
    theme.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
    motion.addEventListener('change', render)
    const resize = new ResizeObserver(() => chart.resize())
    resize.observe(ref.current)
    return () => {
      resize.disconnect()
      theme.disconnect()
      motion.removeEventListener('change', render)
      chart.dispose()
    }
  }, [buckets, models, i18n.language, t])
  return (
    <div
      className={`usage-chart ${models ? 'usage-donut' : ''}`}
      ref={ref}
      role="img"
      aria-label={t(models ? 'settingsModels' : 'settingsTimeline')}
    />
  )
}
