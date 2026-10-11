import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Alert, Button, Space, Switch, Typography } from 'antd';
import { GlobalOutlined } from '@ant-design/icons';
import { useMutation, useQuery } from '@tanstack/react-query';

import { HttpUtil } from '@/utils';
import { keys } from '@/api/queryKeys';

const API = '/panel/api/protect';

export interface FrontView {
  enabled: boolean;
  frontOnly: boolean;
  hasCert: boolean;
  conflict: string;
  port: number;
  panelPort: number;
  running: { listening: boolean; frontOnly: boolean; error: string };
}

export const frontKey = () => [...keys.protect.root(), 'front'] as const;

export function useFront() {
  return useQuery({
    queryKey: frontKey(),
    refetchOnWindowFocus: false,
    queryFn: async () => {
      const msg = await HttpUtil.get<FrontView>(`${API}/front`, undefined, { silent: true });
      if (!msg?.success || !msg.obj) throw new Error(msg?.msg || 'request failed');
      return msg.obj;
    },
  });
}

// publicSiteURL is the decoy's address as visitors see it: no port once it runs on 443.
export function publicSiteURL(front: FrontView | undefined): string {
  if (front?.running.listening) return `https://${window.location.hostname}/`;
  return '/';
}

// FrontSection moves the decoy to port 443 and can close the panel's own port.
export default function FrontSection() {
  const { t } = useTranslation();
  const query = useFront();
  const front = query.data;
  const [restarting, setRestarting] = useState(false);
  const save = useMutation({
    mutationFn: (body: { enabled: boolean; frontOnly: boolean }) =>
      HttpUtil.post(`${API}/front`, body, { headers: { 'Content-Type': 'application/json' } }),
    onSuccess: (msg, body) => {
      if (!msg?.success) return;
      setRestarting(true);
      const base = (window.X_UI_BASE_PATH || '/').replace(/\/+$/, '');
      // After the restart the panel answers on 443 only when its port was closed.
      const next =
        body.enabled && body.frontOnly
          ? `https://${window.location.hostname}${base}/panel/protect`
          : window.location.href;
      window.setTimeout(() => window.location.assign(next), 8000);
    },
  });
  if (!front) return null;
  const base = (window.X_UI_BASE_PATH || '/').replace(/\/+$/, '');
  const panelURL = `https://${window.location.hostname}${base}/`;
  return (
    <div style={{ marginBottom: 16 }}>
      <Typography.Text strong>
        <GlobalOutlined /> {t('pages.protect.frontTitle')}
      </Typography.Text>
      <Typography.Paragraph type="secondary" style={{ margin: '4px 0 8px' }}>
        {t('pages.protect.frontDesc', { port: front.port })}
      </Typography.Paragraph>
      {!front.hasCert && (
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 8 }}
          title={t('pages.protect.frontNoCert')}
        />
      )}
      {front.conflict && (
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 8 }}
          title={t('pages.protect.frontConflict', { port: front.port, name: front.conflict })}
        />
      )}
      {front.enabled && front.running.error && (
        <Alert
          type="error"
          showIcon
          style={{ marginBottom: 8 }}
          title={t('pages.protect.frontFailed', { msg: front.running.error })}
        />
      )}
      {front.running.listening && (
        <Alert
          type="success"
          showIcon
          style={{ marginBottom: 8 }}
          title={t('pages.protect.frontRunning', { url: `https://${window.location.hostname}/` })}
          description={t('pages.protect.frontPanelURL', { url: panelURL })}
        />
      )}
      {restarting && (
        <Alert
          type="info"
          showIcon
          style={{ marginBottom: 8 }}
          title={t('pages.protect.frontRestarting')}
        />
      )}
      <Space orientation="vertical" size={8}>
        <Space wrap>
          <Switch
            checked={front.enabled}
            disabled={!front.hasCert || (!front.enabled && !!front.conflict)}
            loading={save.isPending}
            onChange={(on) => save.mutate({ enabled: on, frontOnly: on && front.frontOnly })}
          />
          <span>{t('pages.protect.frontEnable', { port: front.port })}</span>
        </Space>
        <Space wrap>
          <Switch
            checked={front.frontOnly}
            disabled={!front.enabled || !front.running.listening}
            loading={save.isPending}
            onChange={(on) => save.mutate({ enabled: true, frontOnly: on })}
          />
          <span>{t('pages.protect.frontOnly', { port: front.panelPort })}</span>
        </Space>
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          {t('pages.protect.frontOnlyNote')}
        </Typography.Text>
      </Space>
      {front.running.listening && (
        <div style={{ marginTop: 8 }}>
          <Button
            size="small"
            href={`https://${window.location.hostname}/`}
            target="_blank"
            rel="noreferrer"
          >
            {t('pages.protect.decoyOpen')}
          </Button>
        </div>
      )}
    </div>
  );
}
