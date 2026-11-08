import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'

import LoginView from '../LoginView'

/**
 * The Playwright suite in `frontend/e2e` drives the real login form and relies
 * on these selectors. They are an external suite's implicit API, so a refactor
 * that swaps the shared Input for a raw field (or drops an id) breaks the
 * browser suite silently. Pin them here.
 */
describe('login view e2e DOM contract', () => {
  it('renders exactly one submit button and one field per credential id', () => {
    const { container } = render(
      <MemoryRouter initialEntries={['/login']}>
        <LoginView />
      </MemoryRouter>
    )

    expect(container.querySelectorAll('#email')).toHaveLength(1)
    expect(container.querySelectorAll('#password')).toHaveLength(1)
    expect(container.querySelectorAll('button[type="submit"]')).toHaveLength(1)
    expect(screen.getByLabelText(/email/i)).toBeDefined()
  })

  it('keeps every labelled input reachable from its label wrapper', () => {
    const { container } = render(
      <MemoryRouter initialEntries={['/login']}>
        <LoginView />
      </MemoryRouter>
    )

    // e2e/support.ts resolves inputs as `label -> parent -> input`, so the
    // input must be a descendant of the label's parent element.
    for (const label of Array.from(container.querySelectorAll('label[for]'))) {
      const forId = label.getAttribute('for')
      expect(forId, 'label is missing its for attribute').toBeTruthy()
      const target = label.parentElement?.querySelector(`#${forId}`)
      expect(target, `no #${forId} inside the wrapper of its label`).not.toBeNull()
    }
  })
})
