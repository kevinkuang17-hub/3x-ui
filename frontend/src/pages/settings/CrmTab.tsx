import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Alert, Button, Input, Select, Space, Switch, Typography } from 'antd';
import { CopyOutlined, KeyOutlined, SendOutlined } from '@ant-design/icons';
import { ClipboardManager, HttpUtil } from '@/utils';
import type { AllSetting } from '@/models/setting';
import { SettingListItem } from '@/components/ui';

interface CrmTabProps {
  allSetting: AllSetting;
  updateSetting: (patch: Partial<AllSetting>) => void;
}

const CRM_EVENTS = [
  'client.traffic80',
  'client.depleted',
  'client.expiring',
  'client.expired',
  'line.down',
  'line.up',
  'node.down',
  'node.up',
];

function splitEvents(raw: string): string[] {
  return raw
    .split(',')
    .map((e) => e.trim())
    .filter(Boolean);
}

export default function CrmTab({ allSetting, updateSetting }: CrmTabProps) {
  const { t } = useTranslation();
  const [testing, setTesting] = useState(false);
  const [minting, setMinting] = useState(false);
  const [ssoUrl, setSsoUrl] = useState('');

  async function testWebhook() {
    setTesting(true);
    try {
      await HttpUtil.post('/panel/api/crm/webhook/test');
    } finally {
      setTesting(false);
    }
  }

  async function mintSso() {
    setMinting(true);
    try {
      const msg = await HttpUtil.post<{ url: string }>(
        '/panel/api/crm/sso',
        {},
        { silentSuccess: true },
      );
      if (msg?.success && msg.obj?.url) setSsoUrl(msg.obj.url);
    } finally {
      setMinting(false);
    }
  }

  return (
    <>
      <SettingListItem
        paddings="small"
        title={t('pages.settings.crmWebhookEnable')}
        description={t('pages.settings.crmWebhookEnableDesc')}
      >
        <Switch
          checked={allSetting.crmWebhookEnable}
          onChange={(v) => updateSetting({ crmWebhookEnable: v })}
        />
      </SettingListItem>

      <SettingListItem
        paddings="small"
        title={t('pages.settings.crmWebhookUrl')}
        description={t('pages.settings.crmWebhookUrlDesc')}
      >
        <Input
          value={allSetting.crmWebhookUrl}
          placeholder="https://crm.example.com/hooks/3x-ui"
          onChange={(e) => updateSetting({ crmWebhookUrl: e.target.value })}
        />
      </SettingListItem>

      <SettingListItem
        paddings="small"
        title={t('pages.settings.crmWebhookSecret')}
        description={t('pages.settings.crmWebhookSecretDesc')}
      >
        <Input.Password
          value={allSetting.crmWebhookSecret}
          autoComplete="new-password"
          onChange={(e) => updateSetting({ crmWebhookSecret: e.target.value })}
        />
      </SettingListItem>

      <SettingListItem
        paddings="small"
        title={t('pages.settings.crmWebhookEvents')}
        description={t('pages.settings.crmWebhookEventsDesc')}
      >
        <Select
          mode="multiple"
          allowClear
          style={{ width: '100%' }}
          placeholder={t('pages.settings.crmWebhookEventsAll')}
          value={splitEvents(allSetting.crmWebhookEvents)}
          onChange={(v: string[]) => updateSetting({ crmWebhookEvents: v.join(',') })}
          options={CRM_EVENTS.map((e) => ({ value: e, label: e }))}
        />
      </SettingListItem>

      <Space orientation="vertical" size={8} style={{ width: '100%', marginTop: 16 }}>
        <Button icon={<SendOutlined />} loading={testing} onClick={testWebhook}>
          {t('pages.settings.crmWebhookTest')}
        </Button>
      </Space>

      <SettingListItem
        paddings="small"
        title={t('pages.settings.crmSso')}
        description={t('pages.settings.crmSsoDesc')}
      >
        <Button icon={<KeyOutlined />} loading={minting} onClick={mintSso}>
          {t('pages.settings.crmSsoGenerate')}
        </Button>
      </SettingListItem>
      {ssoUrl && (
        <Alert
          type="info"
          showIcon
          title={
            <Space wrap>
              <Typography.Text code style={{ wordBreak: 'break-all' }}>
                {ssoUrl}
              </Typography.Text>
              <Button
                size="small"
                icon={<CopyOutlined />}
                aria-label={t('copy')}
                onClick={() => ClipboardManager.copyText(ssoUrl)}
              />
            </Space>
          }
          closable={{ onClose: () => setSsoUrl('') }}
        />
      )}
    </>
  );
}
