import { expectMainDocumentCsp } from './support/diagnostics'
import { dashboardProfile, unauthenticatedProfile } from './fixtures/profiles'
import { expect, test } from './fixtures'

test('redirects a protected route to login without an authenticated fixture', async ({
  api,
  page,
}) => {
  api.useProfile(unauthenticatedProfile)

  const response = await page.goto('/vps')

  expectMainDocumentCsp(response)
  await expect(page).toHaveURL(/\/login\?next=%2Fvps$/)
  await expect(page.getByText('候风控制面板')).toBeVisible()
  await expect(page.getByRole('button', { name: '登录' })).toBeVisible()
})

test('enters the protected dashboard with explicit authenticated fixtures', async ({
  api,
  page,
}) => {
  api.useProfile(dashboardProfile())

  const response = await page.goto('/')

  expectMainDocumentCsp(response)
  await expect(page).toHaveURL('/')
  await expect(page.getByRole('heading', { name: '工作台', exact: true })).toBeVisible()
  await expect(page.getByRole('region', { name: '工作台决策面' })).toBeVisible()
})
