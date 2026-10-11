import { useTranslation } from 'react-i18next';
import { Alert, Input, Switch } from 'antd';
import type { AllSetting } from '@/models/setting';
import { SettingListItem } from '@/components/ui';
import SecretInput from './SecretInput';

interface CloudflareTabProps {
  allSetting: AllSetting;
  updateSetting: (patch: Partial<AllSetting>) => void;
}

export default function CloudflareTab({ allSetting, updateSetting }: CloudflareTabProps) {
  const { t } = useTranslation();
  return (
    <>
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 12 }}
        title={t('pages.settings.cfIntro')}
      />
      <SettingListItem
        paddings="small"
        title={t('pages.settings.cfApiToken')}
        description={t('pages.settings.cfApiTokenDesc')}
      >
        <SecretInput
          value={allSetting.cfApiToken}
          configured={allSetting.hasCfApiToken}
          clearArmed={allSetting.clearCfApiToken}
          placeholder={t('pages.settings.secretConfigured')}
          onChange={(v) => updateSetting({ cfApiToken: v.trim() })}
          onClearArmedChange={(armed) => updateSetting({ clearCfApiToken: armed })}
        />
      </SettingListItem>
      <SettingListItem
        paddings="small"
        title={t('pages.settings.cfZone')}
        description={t('pages.settings.cfZoneDesc')}
      >
        <Input
          value={allSetting.cfZone}
          placeholder="example.com"
          onChange={(e) => updateSetting({ cfZone: e.target.value.trim() })}
        />
      </SettingListItem>
      <SettingListItem
        paddings="small"
        title={t('pages.settings.cfAutoDns')}
        description={t('pages.settings.cfAutoDnsDesc')}
      >
        <Switch checked={allSetting.cfAutoDns} onChange={(v) => updateSetting({ cfAutoDns: v })} />
      </SettingListItem>
    </>
  );
}
