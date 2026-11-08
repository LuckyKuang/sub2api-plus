import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { GripVertical, Pencil, Plus, Trash2, X } from 'lucide-react'
import { adminAPI } from '@/api/admin'
import type { UserAttributeDefinition, UserAttributeOption, UserAttributeType } from '@/types'
import { useAppStore } from '@/stores/app'
import { Button, ConfirmDialog, Input, Modal, Select } from '@/components/ui'

const ATTRIBUTE_TYPES: UserAttributeType[] = [
  'text',
  'textarea',
  'number',
  'email',
  'url',
  'date',
  'select',
  'multi_select'
]

interface AttributeFormState {
  key: string
  name: string
  type: UserAttributeType
  description: string
  placeholder: string
  required: boolean
  enabled: boolean
  options: UserAttributeOption[]
}

const EMPTY_FORM: AttributeFormState = {
  key: '',
  name: '',
  type: 'text',
  description: '',
  placeholder: '',
  required: false,
  enabled: true,
  options: []
}

export function UserAttributesConfigModal({
  show,
  onClose,
  onSaved
}: {
  show: boolean
  onClose: () => void
  onSaved: () => void
}) {
  const { t } = useTranslation()
  const showError = useAppStore((state) => state.showError)
  const showSuccess = useAppStore((state) => state.showSuccess)

  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [attributes, setAttributes] = useState<UserAttributeDefinition[]>([])
  const [showEditModal, setShowEditModal] = useState(false)
  const [showDeleteDialog, setShowDeleteDialog] = useState(false)
  const [editingAttribute, setEditingAttribute] = useState<UserAttributeDefinition | null>(null)
  const [deletingAttribute, setDeletingAttribute] = useState<UserAttributeDefinition | null>(null)
  const [form, setForm] = useState<AttributeFormState>(EMPTY_FORM)

  const loadAttributes = async () => {
    setLoading(true)
    try {
      setAttributes(await adminAPI.userAttributes.listDefinitions())
    } catch (error) {
      showError(
        (error as { response?: { data?: { detail?: string } } })?.response?.data?.detail ||
          t('admin.users.attributes.failedToLoad')
      )
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    if (show) void loadAttributes()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [show])

  const openCreateModal = () => {
    setEditingAttribute(null)
    setForm(EMPTY_FORM)
    setShowEditModal(true)
  }

  const openEditModal = (attr: UserAttributeDefinition) => {
    setEditingAttribute(attr)
    setForm({
      key: attr.key,
      name: attr.name,
      type: attr.type,
      description: attr.description || '',
      placeholder: attr.placeholder || '',
      required: attr.required,
      enabled: attr.enabled,
      options: attr.options ? attr.options.map((opt) => ({ ...opt })) : []
    })
    setShowEditModal(true)
  }

  const closeEditModal = () => {
    setShowEditModal(false)
    setEditingAttribute(null)
  }

  const addOption = () => {
    setForm((prev) => ({ ...prev, options: [...prev.options, { value: '', label: '' }] }))
  }

  const removeOption = (index: number) => {
    setForm((prev) => ({ ...prev, options: prev.options.filter((_, i) => i !== index) }))
  }

  const updateOption = (index: number, patch: Partial<UserAttributeOption>) => {
    setForm((prev) => ({
      ...prev,
      options: prev.options.map((option, i) => (i === index ? { ...option, ...patch } : option))
    }))
  }

  const handleSave = async () => {
    if (!form.key.trim()) {
      showError(t('admin.users.attributes.keyRequired'))
      return
    }
    if (!form.name.trim()) {
      showError(t('admin.users.attributes.nameRequired'))
      return
    }
    if ((form.type === 'select' || form.type === 'multi_select') && form.options.length === 0) {
      showError(t('admin.users.attributes.optionsRequired'))
      return
    }
    setSaving(true)
    try {
      const data = {
        key: form.key,
        name: form.name,
        type: form.type,
        description: form.description || undefined,
        placeholder: form.placeholder || undefined,
        required: form.required,
        enabled: form.enabled,
        options: form.type === 'select' || form.type === 'multi_select' ? form.options : undefined
      }

      if (editingAttribute) {
        await adminAPI.userAttributes.updateDefinition(editingAttribute.id, data)
        showSuccess(t('admin.users.attributes.updated'))
      } else {
        await adminAPI.userAttributes.createDefinition(data)
        showSuccess(t('admin.users.attributes.created'))
      }

      closeEditModal()
      void loadAttributes()
      onSaved()
    } catch (error) {
      const msg = editingAttribute
        ? t('admin.users.attributes.failedToUpdate')
        : t('admin.users.attributes.failedToCreate')
      showError(
        (error as { response?: { data?: { detail?: string } } })?.response?.data?.detail || msg
      )
    } finally {
      setSaving(false)
    }
  }

  const confirmDelete = (attr: UserAttributeDefinition) => {
    setDeletingAttribute(attr)
    setShowDeleteDialog(true)
  }

  const handleDelete = async () => {
    if (!deletingAttribute) return

    try {
      await adminAPI.userAttributes.deleteDefinition(deletingAttribute.id)
      showSuccess(t('admin.users.attributes.deleted'))
      setShowDeleteDialog(false)
      setDeletingAttribute(null)
      void loadAttributes()
      onSaved()
    } catch (error) {
      showError(
        (error as { response?: { data?: { detail?: string } } })?.response?.data?.detail ||
          t('admin.users.attributes.failedToDelete')
      )
    }
  }

  return (
    <>
      <Modal
        open={show}
        onClose={onClose}
        title={t('admin.users.attributes.title')}
        size="xl"
        footer={
          <div className="flex justify-end">
            <Button variant="secondary" onClick={onClose}>
              {t('common.close')}
            </Button>
          </div>
        }
      >
        <div className="space-y-4">
          <div className="flex items-center justify-between">
            <p className="text-sm surface-muted">{t('admin.users.attributes.description')}</p>
            <Button variant="primary" size="sm" onClick={openCreateModal}>
              <Plus className="mr-1.5 h-4 w-4" strokeWidth={2} />
              {t('admin.users.attributes.addAttribute')}
            </Button>
          </div>

          {loading ? (
            <div className="flex justify-center py-12">
              <svg className="h-8 w-8 animate-spin text-primary-500" fill="none" viewBox="0 0 24 24">
                <circle opacity="0.25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                <path opacity="0.75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z" />
              </svg>
            </div>
          ) : attributes.length === 0 ? (
            <div className="py-12 text-center">
              <svg className="mx-auto h-12 w-12 text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth="1">
                <path strokeLinecap="round" strokeLinejoin="round" d="M9.568 3H5.25A2.25 2.25 0 003 5.25v4.318c0 .597.237 1.17.659 1.591l9.581 9.581c.699.699 1.78.872 2.607.33a18.095 18.095 0 005.223-5.223c.542-.827.369-1.908-.33-2.607L11.16 3.66A2.25 2.25 0 009.568 3z" />
                <path strokeLinecap="round" strokeLinejoin="round" d="M6 6h.008v.008H6V6z" />
              </svg>
              <p className="mt-2 text-sm surface-muted">{t('admin.users.attributes.noAttributes')}</p>
              <p className="text-xs surface-muted">{t('admin.users.attributes.noAttributesHint')}</p>
            </div>
          ) : (
            <div className="max-h-96 space-y-2 overflow-y-auto">
              {attributes.map((attr) => (
                <div
                  key={attr.id}
                  className="flex items-center gap-3 rounded-lg border border-gray-200 surface-inset p-3 dark:border-dark-600"
                >
                  <div className="cursor-move hover:text-gray-600 dark:hover:text-gray-300 surface-muted" title={t('admin.users.attributes.dragToReorder')}>
                    <GripVertical className="h-4 w-4" />
                  </div>

                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <span className="font-medium text-gray-900 dark:text-white">{attr.name}</span>
                      <span className="rounded surface-inset px-1.5 py-0.5 font-mono text-xs surface-muted">{attr.key}</span>
                      {attr.required && <span className="badge badge-danger text-xs">{t('admin.users.attributes.required')}</span>}
                      {!attr.enabled && <span className="badge badge-gray text-xs">{t('common.disabled')}</span>}
                    </div>
                    <div className="mt-0.5 flex items-center gap-2 text-xs surface-muted">
                      <span className="badge badge-gray">{t(`admin.users.attributes.types.${attr.type}`)}</span>
                      {attr.description && <span className="truncate">{attr.description}</span>}
                    </div>
                  </div>

                  <div className="flex items-center gap-1">
                    <button
                      onClick={() => openEditModal(attr)}
                      className="rounded-lg p-1.5 hover:surface-hover hover:text-primary-600 dark:hover:text-primary-400 surface-control surface-muted"
                      title={t('common.edit')}
                    >
                      <Pencil className="h-4 w-4" />
                    </button>
                    <button
                      onClick={() => confirmDelete(attr)}
                      className="rounded-lg p-1.5 hover:bg-red-50/25 hover:text-red-600 dark:hover:bg-red-900/20 dark:hover:text-red-400 surface-control surface-muted"
                      title={t('common.delete')}
                    >
                      <Trash2 className="h-4 w-4" />
                    </button>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      </Modal>

      <Modal
        open={showEditModal}
        onClose={closeEditModal}
        title={editingAttribute ? t('admin.users.attributes.editAttribute') : t('admin.users.attributes.addAttribute')}
        footer={
          <div className="flex justify-end gap-3">
            <Button variant="secondary" onClick={closeEditModal}>
              {t('common.cancel')}
            </Button>
            <Button variant="primary" loading={saving} onClick={handleSave}>
              {saving ? t('common.saving') : editingAttribute ? t('common.update') : t('common.create')}
            </Button>
          </div>
        }
      >
        <div className="space-y-4">
          <div>
            <label className="input-label">{t('admin.users.attributes.key')}</label>
            <Input
              type="text"
              required
              pattern="^[a-zA-Z][a-zA-Z0-9_]*$"
              className="font-mono"
              value={form.key}
              onValueChange={(value) => setForm((prev) => ({ ...prev, key: value }))}
              placeholder={t('admin.users.attributes.keyHint')}
              disabled={!!editingAttribute}
            />
            <p className="input-hint">{t('admin.users.attributes.keyHint')}</p>
          </div>

          <div>
            <label className="input-label">{t('admin.users.attributes.name')}</label>
            <Input
              type="text"
              required
              value={form.name}
              onValueChange={(value) => setForm((prev) => ({ ...prev, name: value }))}
              placeholder={t('admin.users.attributes.nameHint')}
            />
          </div>

          <div>
            <label className="input-label">{t('admin.users.attributes.type')}</label>
            <Select
              searchable={false}
              value={form.type}
              options={ATTRIBUTE_TYPES.map((type) => ({
                value: type,
                label: t(`admin.users.attributes.types.${type}`)
              }))}
              onValueChange={(value) => setForm((prev) => ({ ...prev, type: String(value) as UserAttributeType }))}
            />
          </div>

          {(form.type === 'select' || form.type === 'multi_select') && (
            <div className="space-y-2">
              <label className="input-label">{t('admin.users.attributes.options')}</label>
              {form.options.map((option, index) => (
                <div key={`option-${index}`} className="flex items-center gap-2">
                  <Input
                    type="text"
                    className="flex-1 font-mono text-sm"
                    value={option.value}
                    onValueChange={(value) => updateOption(index, { value })}
                    placeholder={t('admin.users.attributes.optionValue')}
                    required
                  />
                  <Input
                    type="text"
                    className="flex-1 text-sm"
                    value={option.label}
                    onValueChange={(value) => updateOption(index, { label: value })}
                    placeholder={t('admin.users.attributes.optionLabel')}
                    required
                  />
                  <button
                    type="button"
                    onClick={() => removeOption(index)}
                    className="rounded-lg p-1.5 hover:bg-red-50/25 hover:text-red-600 surface-control surface-muted"
                  >
                    <X className="h-4 w-4" strokeWidth={2} />
                  </button>
                </div>
              ))}
              <Button variant="secondary" size="sm" onClick={addOption}>
                <Plus className="mr-1 h-4 w-4" strokeWidth={2} />
                {t('admin.users.attributes.addOption')}
              </Button>
            </div>
          )}

          <div>
            <label className="input-label">{t('admin.users.attributes.fieldDescription')}</label>
            <Input
              type="text"
              value={form.description}
              onValueChange={(value) => setForm((prev) => ({ ...prev, description: value }))}
              placeholder={t('admin.users.attributes.fieldDescriptionHint')}
            />
          </div>

          <div>
            <label className="input-label">{t('admin.users.attributes.placeholder')}</label>
            <Input
              type="text"
              value={form.placeholder}
              onValueChange={(value) => setForm((prev) => ({ ...prev, placeholder: value }))}
              placeholder={t('admin.users.attributes.placeholderHint')}
            />
          </div>

          <div className="flex items-center gap-6">
            <label className="flex items-center gap-2">
              <input
                type="checkbox"
                checked={form.required}
                onChange={(event) => setForm((prev) => ({ ...prev, required: event.target.checked }))}
                className="h-4 w-4 rounded border-gray-300 text-primary-600"
              />
              <span className="text-sm surface-muted">{t('admin.users.attributes.required')}</span>
            </label>
            <label className="flex items-center gap-2">
              <input
                type="checkbox"
                checked={form.enabled}
                onChange={(event) => setForm((prev) => ({ ...prev, enabled: event.target.checked }))}
                className="h-4 w-4 rounded border-gray-300 text-primary-600"
              />
              <span className="text-sm surface-muted">{t('admin.users.attributes.enabled')}</span>
            </label>
          </div>
        </div>
      </Modal>

      <ConfirmDialog
        open={showDeleteDialog}
        title={t('admin.users.attributes.deleteAttribute')}
        message={t('admin.users.attributes.deleteConfirm', { name: deletingAttribute?.name })}
        confirmText={t('common.delete')}
        variant="danger"
        onConfirm={handleDelete}
        onCancel={() => setShowDeleteDialog(false)}
      />
    </>
  )
}

export default UserAttributesConfigModal
