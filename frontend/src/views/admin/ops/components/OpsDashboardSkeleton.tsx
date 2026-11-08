export interface OpsDashboardSkeletonProps {
  fullscreen?: boolean
}

export function OpsDashboardSkeleton({ fullscreen = false }: OpsDashboardSkeletonProps) {
  return (
    <div className="space-y-6">
      <div className={`card ${fullscreen ? 'p-8' : 'p-6'}`}>
        <div className="flex flex-wrap items-center justify-between gap-4 border-b border-gray-100 pb-4 dark:border-dark-700">
          <div className="space-y-2">
            <div className="h-6 w-44 animate-pulse rounded bg-gray-200 dark:bg-dark-700" />
            <div className="h-3 w-80 max-w-full animate-pulse rounded bg-gray-100 dark:bg-dark-700/70" />
          </div>
          {!fullscreen && (
            <div className="flex flex-wrap items-center gap-3">
              <div className="h-9 w-[140px] animate-pulse rounded-xl bg-gray-200 dark:bg-dark-700" />
              <div className="h-9 w-[160px] animate-pulse rounded-xl bg-gray-200 dark:bg-dark-700" />
              <div className="h-9 w-[150px] animate-pulse rounded-xl bg-gray-200 dark:bg-dark-700" />
              <div className="h-9 w-9 animate-pulse rounded-xl bg-gray-200 dark:bg-dark-700" />
              <div className="h-9 w-28 animate-pulse rounded-xl bg-gray-200 dark:bg-dark-700" />
              <div className="h-9 w-28 animate-pulse rounded-xl bg-gray-200 dark:bg-dark-700" />
              <div className="h-9 w-9 animate-pulse rounded-xl bg-gray-200 dark:bg-dark-700" />
            </div>
          )}
        </div>

        <div className="mt-6 grid grid-cols-1 gap-6 lg:grid-cols-12">
          <div className="rounded-2xl surface-inset p-4 lg:col-span-5">
            <div className="grid h-full grid-cols-1 gap-6 md:grid-cols-[200px_1fr] md:items-center">
              <div className="h-28 animate-pulse rounded-xl bg-gray-100 dark:bg-dark-700/70" />
              <div className="space-y-4">
                <div className="h-4 w-32 animate-pulse rounded bg-gray-200 dark:bg-dark-700" />
                <div className="grid grid-cols-2 gap-3">
                  {Array.from({ length: 4 }).map((_, index) => (
                    <div key={index} className="h-14 animate-pulse rounded-xl bg-gray-100 dark:bg-dark-700/70" />
                  ))}
                </div>
              </div>
            </div>
          </div>

          <div className="lg:col-span-7">
            <div className="grid h-full grid-cols-1 content-center gap-4 sm:grid-cols-2 lg:grid-cols-3">
              {Array.from({ length: 7 }).map((_, index) => (
                <div key={index} className="h-20 animate-pulse rounded-2xl bg-gray-50 dark:bg-dark-900/30" />
              ))}
            </div>
          </div>
        </div>
      </div>

      <div className="grid grid-cols-1 gap-6 lg:grid-cols-4">
        {['lg:col-span-1', 'lg:col-span-1', 'lg:col-span-2'].map((span, index) => (
          <div key={index} className={`min-h-[360px] card ${span} ${fullscreen ? 'p-8' : 'p-6'}`}>
            <div className="h-4 w-44 animate-pulse rounded bg-gray-200 dark:bg-dark-700" />
            <div className="mt-6 h-72 animate-pulse rounded-2xl bg-gray-100 dark:bg-dark-700/70" />
          </div>
        ))}
      </div>

      <div className="grid grid-cols-1 gap-6 md:grid-cols-3">
        {Array.from({ length: 3 }).map((_, index) => (
          <div key={index} className={`card ${fullscreen ? 'p-8' : 'p-6'}`}>
            <div className="h-4 w-44 animate-pulse rounded bg-gray-200 dark:bg-dark-700" />
            <div className="mt-6 h-56 animate-pulse rounded-2xl bg-gray-100 dark:bg-dark-700/70" />
          </div>
        ))}
      </div>

      <div className={`card ${fullscreen ? 'p-8' : 'p-6'}`}>
        <div className="flex flex-wrap items-center justify-between gap-4">
          <div className="h-4 w-48 animate-pulse rounded bg-gray-200 dark:bg-dark-700" />
          {!fullscreen && (
            <div className="flex flex-wrap items-center gap-2">
              <div className="h-9 w-[140px] animate-pulse rounded-xl bg-gray-200 dark:bg-dark-700" />
              <div className="h-9 w-[120px] animate-pulse rounded-xl bg-gray-200 dark:bg-dark-700" />
              <div className="h-9 w-[120px] animate-pulse rounded-xl bg-gray-200 dark:bg-dark-700" />
            </div>
          )}
        </div>

        <div className="mt-6 space-y-3">
          {Array.from({ length: 6 }).map((_, index) => (
            <div key={index} className="flex items-center justify-between gap-4 rounded-2xl surface-inset p-4">
              <div className="flex-1 space-y-2">
                <div className="h-3 w-56 animate-pulse rounded bg-gray-200 dark:bg-dark-700" />
                <div className="h-3 w-80 max-w-full animate-pulse rounded bg-gray-100 dark:bg-dark-700/70" />
              </div>
              <div className="h-7 w-20 animate-pulse rounded-xl bg-gray-200 dark:bg-dark-700" />
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}

export default OpsDashboardSkeleton
