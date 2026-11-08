import { useEffect, useRef } from 'react'
import {
  Chart as ChartJS,
  ArcElement,
  BarController,
  BarElement,
  CategoryScale,
  DoughnutController,
  Filler,
  Legend,
  LineController,
  LineElement,
  LinearScale,
  LogarithmicScale,
  PointElement,
  Tooltip,
  type ChartData,
  type ChartOptions
} from 'chart.js'

// chart.js v4 ships unbundled: elements alone are not enough, each chart kind
// needs its controller registered or `new Chart` throws
// ""<kind>" is not a registered controller".
ChartJS.register(
  CategoryScale,
  LinearScale,
  LogarithmicScale,
  PointElement,
  LineElement,
  BarElement,
  ArcElement,
  LineController,
  BarController,
  DoughnutController,
  Tooltip,
  Legend,
  Filler
)

export type ChartKind = 'line' | 'doughnut' | 'bar'

export interface ChartCanvasProps {
  kind: ChartKind
  data: ChartData<ChartKind>
  options?: ChartOptions<ChartKind>
}

/**
 * Minimal chart.js wrapper used by the ported admin charts. The Vue app used
 * `vue-chartjs`; the registration and option shapes are unchanged.
 *
 * chart.js refuses to take over a canvas that a previous instance still owns,
 * so any chart already bound to the canvas is destroyed before a new one is
 * created. Without that guard a remount (React strict mode, or a chart swapped
 * without unmounting) throws "Canvas is already in use".
 */
export function ChartCanvas({ kind, data, options }: ChartCanvasProps) {
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const chartRef = useRef<ChartJS | null>(null)

  useEffect(() => {
    const canvas = canvasRef.current
    if (!canvas) return

    ChartJS.getChart(canvas)?.destroy()

    const chart = new ChartJS(canvas, {
      type: kind,
      data: data as never,
      options: { responsive: true, maintainAspectRatio: false, ...(options ?? {}) } as never
    })
    chartRef.current = chart

    return () => {
      chart.destroy()
      if (chartRef.current === chart) chartRef.current = null
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [kind])

  useEffect(() => {
    const chart = chartRef.current
    if (!chart) return
    chart.data = data as never
    chart.options = { responsive: true, maintainAspectRatio: false, ...(options ?? {}) } as never
    chart.update()
  }, [data, options])

  return <canvas ref={canvasRef} />
}

export default ChartCanvas
