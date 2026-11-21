import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'

import { ProfilesTab } from './components/profiles-tab'

export function Jailbreak() {
  const { t } = useTranslation()

  return (
    <SectionPageLayout fixedContent>
      <SectionPageLayout.Title>
        {t('Request Rules')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <ProfilesTab />

      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}