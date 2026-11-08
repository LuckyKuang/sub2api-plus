import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Icon } from '@/components/Icon'
import { Input } from '@/components/ui'

export interface ModelWhitelistSelectorProps {
  value: string[]
  onValueChange?: (models: string[]) => void
  className?: string
}

/**
 * Model filter editor: keeps the selected models as removable chips and adds
 * new ones through the inline field. Selection is free-form because the model
 * filter applies to any model name the gateway can route.
 */
export function ModelWhitelistSelector({ value, onValueChange, className }: ModelWhitelistSelectorProps) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState('')

  const models = useMemo(() => value, [value])

  const emit = (next: string[]) => {
    onValueChange?.(next)
  }

  const removeModel = (model: string) => {
    emit(models.filter((item) => item !== model))
  }

  const addModel = () => {
    const model = draft.trim()
    if (!model) return
    if (models.includes(model)) {
      setDraft('')
      return
    }
    emit([...models, model])
    setDraft('')
  }

  return (
    <div className={className}>
      {models.length > 0 && (
        <div className="mb-3 grid grid-cols-2 gap-1.5">
          {models.map((model) => (
            <span
              key={model}
              className="inline-flex items-center justify-between gap-1 rounded surface-inset px-2 py-1 text-xs surface-muted"
            >
              <span className="truncate">{model}</span>
              <button
                type="button"
                onClick={() => removeModel(model)}
                className="shrink-0 rounded-full hover:surface-hover dark:hover:bg-dark-500 surface-control"
                aria-label={t('admin.riskControl.deleteApiKey')}
              >
                <Icon name="x" size="xs" className="h-3.5 w-3.5" strokeWidth={2} />
              </button>
            </span>
          ))}
        </div>
      )}
      <div className="flex items-center gap-2">
        <Input
          value={draft}
          onValueChange={setDraft}
          placeholder={t('admin.accounts.searchModels')}
          onKeyDown={(event) => {
            if (event.key === 'Enter') {
              event.preventDefault()
              addModel()
            }
          }}
        />
        <button type="button" className="btn btn-secondary shrink-0" onClick={addModel}>
          <Icon name="plus" size="sm" />
        </button>
      </div>
    </div>
  )
}

export default ModelWhitelistSelector
