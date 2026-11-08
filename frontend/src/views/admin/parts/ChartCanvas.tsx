import { useEffect, useRef } from 'react'
import {
  Chart as ChartJS,
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  BarElement,
  ArcElement,
  Tooltip,
  Legend,
  Filler,
  type ChartData,
  type ChartOptions
} from 'chart.js'

ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, BarElement, ArcElement, Tooltip, Legend, Filler)

export type ChartKind = 'line' | 'doughnut' | 'bar'

export interface ChartCanvasProps {
  kind: ChartKind
  data: ChartData<ChartKind>
  options?: ChartOptions<ChartKind>
}

/**
 * Minimal chart.js wrapper used by the ported admin charts. The Vue app used
 * `vue-chartjs`; the registration and option shapes are unchanged.
 */
export function ChartCanvas({ kind, data, options }: ChartCanvasProps) {
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const chartRef = useRef<ChartJS | null>(null)

  useEffect(() => {
    if (!canvasRef.current) return
    chartRef.current = new ChartJS(canvasRef.current, {
      type: kind,
      data: data as never,
      options: { responsive: true, maintainAspectRatio: false, ...(options ?? {}) } as never
    })
    return () => {
      chartRef.current?.destroy()
      chartRef.current = null
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
