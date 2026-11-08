import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { accountsAPI } from '@/api/admin/accounts'
import { extractApiErrorMessage } from '@/utils/apiError'
import type { Account } from '@/types'

const MIN_PRIORITY = 1
const MAX_PRIORITY = 9999
// 连续点击时合并为一次保存，避免每次 +/- 都打一次接口
const SAVE_DEBOUNCE_MS = 450

export interface AccountPriorityCellProps {
  account: Account
  onUpdated: (account: Account) => void
  onError: (message: string) => void
}

export function AccountPriorityCell({ account, onUpdated, onError }: AccountPriorityCellProps) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState(account.priority)
  const [saving, setSaving] = useState(false)
  const [editing, setEditing] = useState(false)
  const [inputValue, setInputValue] = useState('')
  const inputRef = useRef<HTMLInputElement | null>(null)
  const saveTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const draftRef = useRef(draft)
  draftRef.current = draft
  const saveRef = useRef<() => Promise<void>>(async () => {})

  const dirty = draft !== account.priority

  useEffect(() => {
    if (!saving && !saveTimer.current && !editing) setDraft(account.priority)
  }, [account.priority, saving, editing])

  useEffect(
    () => () => {
      // 卸载前把还没发出的改动落库，避免翻页/刷新时丢失
      if (saveTimer.current) void saveRef.current()
    },
    []
  )

  const clamp = (value: number) => Math.min(MAX_PRIORITY, Math.max(MIN_PRIORITY, Math.round(value)))

  const clearTimer = () => {
    if (saveTimer.current) {
      clearTimeout(saveTimer.current)
      saveTimer.current = null
    }
  }

  const save = useCallback(async () => {
    clearTimer()
    const target = draftRef.current
    if (target === account.priority) return
    setSaving(true)
    try {
      const updated = await accountsAPI.update(account.id, { priority: target })
      onUpdated(updated)
    } catch (error) {
      setDraft(account.priority)
      onError(extractApiErrorMessage(error, t('admin.accounts.priorityQuick.failed')))
    } finally {
      setSaving(false)
    }
  }, [account.id, account.priority, onUpdated, onError, t])

  const scheduleSave = () => {
    clearTimer()
    saveTimer.current = setTimeout(() => void save(), SAVE_DEBOUNCE_MS)
  }
  saveRef.current = save

  const step = (delta: number) => {
    if (saving) return
    const next = clamp(draft + delta)
    if (next === draft) return
    setDraft(next)
    scheduleSave()
  }

  const startEditing = () => {
    if (saving) return
    clearTimer()
    setInputValue(String(draft))
    setEditing(true)
    requestAnimationFrame(() => {
      inputRef.current?.focus()
      inputRef.current?.select()
    })
  }

  const nudgeInput = (delta: number) => {
    const current = Number.parseInt(inputValue, 10)
    setInputValue(String(clamp((Number.isFinite(current) ? current : draft) + delta)))
  }

  const commitInput = () => {
    if (!editing) return
    setEditing(false)
    const parsed = Number.parseInt(inputValue.trim(), 10)
    if (Number.isFinite(parsed)) setDraft(clamp(parsed))
    void save()
  }

  const cancelInput = () => {
    setEditing(false)
    if (!saving) setDraft(account.priority)
  }

  return (
    <div
      className={cn(
        'group/priority inline-flex h-7 items-center rounded-lg border transition-colors duration-150',
        editing || dirty
          ? 'border-primary-300 bg-primary-50/70 dark:border-primary-700 dark:bg-primary-900/20'
          : 'border-transparent hover:border-gray-200 hover:surface-hover dark:hover:border-dark-600 dark:hover:surface-hover'
      )}
      data-testid="account-priority-cell"
    >
      <button
        type="button"
        className={cn(
          'flex h-full w-6 items-center justify-center rounded-l-lg opacity-0 transition hover:surface-hover hover:text-primary-600 focus:opacity-100 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 disabled:cursor-not-allowed disabled:text-gray-300 disabled:hover:bg-transparent disabled:hover:text-gray-300 dark:disabled:text-dark-600 dark:disabled:hover:text-dark-600 group-hover/priority:opacity-100 group-focus-within/priority:opacity-100 dark:hover:text-primary-400 [@media(hover:none)]:opacity-100 surface-control surface-muted',
          (editing || dirty) && '!opacity-100'
        )}
        disabled={saving || draft <= MIN_PRIORITY}
        title={t('admin.accounts.priorityQuick.raise')}
        aria-label={t('admin.accounts.priorityQuick.raise')}
        data-testid="account-priority-decrement"
        onClick={() => step(-1)}
      >
        <svg className="h-3 w-3" viewBox="0 0 12 12" fill="none" stroke="currentColor" strokeWidth="1.75" strokeLinecap="round">
          <path d="M2.5 6h7" />
        </svg>
      </button>

      {editing ? (
        <input
          ref={inputRef}
          value={inputValue}
          onChange={(event) => setInputValue(event.target.value)}
          type="text"
          inputMode="numeric"
          className="h-full w-10 border-0 bg-transparent p-0 text-center font-mono text-sm tabular-nums text-gray-900 focus:outline-none focus:ring-0 dark:text-white"
          aria-label={t('admin.accounts.columns.priority')}
          data-testid="account-priority-input"
          onKeyDown={(event) => {
            if (event.key === 'Enter') {
              event.preventDefault()
              commitInput()
            } else if (event.key === 'Escape') {
              event.preventDefault()
              cancelInput()
            } else if (event.key === 'ArrowUp') {
              event.preventDefault()
              nudgeInput(1)
            } else if (event.key === 'ArrowDown') {
              event.preventDefault()
              nudgeInput(-1)
            }
          }}
          onBlur={commitInput}
        />
      ) : (
        <button
          type="button"
          className={cn(
            'relative flex h-full min-w-[2rem] items-center justify-center px-1 font-mono text-sm tabular-nums transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 surface-control',
            dirty
              ? 'font-semibold text-primary-600 dark:text-primary-400'
              : 'text-gray-700 hover:text-gray-900 dark:text-gray-300 dark:hover:text-white'
          )}
          disabled={saving}
          title={t('admin.accounts.priorityQuick.editHint')}
          data-testid="account-priority-value"
          onClick={startEditing}
        >
          {draft}
          {saving && (
            <span
              className="console-data-mark absolute -right-0.5 top-1 h-1.5 w-1.5 animate-pulse rounded-full bg-primary-500"
              data-testid="account-priority-saving"
            />
          )}
        </button>
      )}

      <button
        type="button"
        className={cn(
          'flex h-full w-6 items-center justify-center rounded-r-lg opacity-0 transition hover:surface-hover hover:text-primary-600 focus:opacity-100 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 disabled:cursor-not-allowed disabled:text-gray-300 disabled:hover:bg-transparent disabled:hover:text-gray-300 dark:disabled:text-dark-600 dark:disabled:hover:text-dark-600 group-hover/priority:opacity-100 group-focus-within/priority:opacity-100 dark:hover:text-primary-400 [@media(hover:none)]:opacity-100 surface-control surface-muted',
          (editing || dirty) && '!opacity-100'
        )}
        disabled={saving || draft >= MAX_PRIORITY}
        title={t('admin.accounts.priorityQuick.lower')}
        aria-label={t('admin.accounts.priorityQuick.lower')}
        data-testid="account-priority-increment"
        onClick={() => step(1)}
      >
        <svg className="h-3 w-3" viewBox="0 0 12 12" fill="none" stroke="currentColor" strokeWidth="1.75" strokeLinecap="round">
          <path d="M2.5 6h7M6 2.5v7" />
        </svg>
      </button>
    </div>
  )
}

export default AccountPriorityCell
