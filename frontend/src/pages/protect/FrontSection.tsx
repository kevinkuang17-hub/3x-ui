import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Alert, Button, InputNumber, Space, Switch, Typography } from 'antd';
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
  running: { listening: boolean; frontOnly: boolean; error: string; addr?: string };
  loopback?: boolean;
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

// siteOrigin is the website's address: no port on 443, like any other site.
export function siteOrigin(port: number): string {
  const host = window.location.hostname;
  return port === 443 ? `https://${host}` : `https://${host}:${port}`;
}

// publicPort is where visitors reach the website: 443 through REALITY when it sits behind it.
export function publicPort(front: FrontView): number {
  return front.loopback ? 443 : front.port;
}

// publicSiteURL is the decoy's address as visitors see it once the website runs.
export function publicSiteURL(front: FrontView | undefined): string {
  if (front?.running.listening) return `${siteOrigin(publicPort(front))}/`;
  return '/';
}

// onFrontPort tells whether this page itself was opened through the website port.
function onFrontPort(port: number): boolean {
  if (window.location.protocol !== 'https:') return false;
  const here = window.location.port || '443';
  return here === String(port);
}

// FrontSection moves the decoy to port 443 and can close the panel's own port.
export default function FrontSection() {
  const { t } = useTranslation();
  const query = useFront();
  const front = query.data;
  const [restarting, setRestarting] = useState(false);
  const [portDraft, setPortDraft] = useState<number | null>(null);
  const save = useMutation({
    mutationFn: (body: {
      enabled: boolean;
      frontOnly: boolean;
      port: number;
      loopback?: boolean;
    }) => HttpUtil.post(`${API}/front`, body, { headers: { 'Content-Type': 'application/json' } }),
    onSuccess: (msg, body) => {
      if (!msg?.success) return;
      setRestarting(true);
      const base = (window.X_UI_BASE_PATH || '/').replace(/\/+$/, '');
      const host = window.location.hostname;
      // After the restart the panel answers on the website port only when its port was closed, and
      // on its own port only once the website is off.
      let next = window.location.href;
      if (body.enabled && body.frontOnly) next = `${siteOrigin(body.port)}${base}/panel/protect`;
      else if (!body.enabled && front && onFrontPort(front.port))
        next = `https://${host}:${front.panelPort}${base}/panel/protect`;
      window.setTimeout(() => window.location.assign(next), 8000);
    },
  });
  if (!front) return null;
  const base = (window.X_UI_BASE_PATH || '/').replace(/\/+$/, '');
  const panelURL = `${siteOrigin(publicPort(front))}${base}/`;
  const onFront = onFrontPort(publicPort(front));
  const port = portDraft ?? front.port;
  const portChanged = port !== front.port;
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
          title={t('pages.protect.frontRunning', { url: `${siteOrigin(publicPort(front))}/` })}
          description={t('pages.protect.frontPanelURL', { url: panelURL })}
        />
      )}
      {front.loopback && (
        <Alert
          type="info"
          showIcon
          style={{ marginBottom: 8 }}
          title={t('pages.protect.frontBehindReality', { addr: front.running.addr || front.port })}
          action={
            <Button
              size="small"
              loading={save.isPending}
              onClick={() =>
                save.mutate({ enabled: true, frontOnly: false, port: 443, loopback: false })
              }
            >
              {t('pages.protect.frontMakePublic')}
            </Button>
          }
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
            onChange={(on) => save.mutate({ enabled: on, frontOnly: on && front.frontOnly, port })}
          />
          <span>{t('pages.protect.frontEnable', { port })}</span>
        </Space>
        <Space wrap>
          <span>{t('pages.protect.frontPort')}</span>
          <InputNumber
            min={1}
            max={65535}
            value={port}
            disabled={front.frontOnly || !!front.loopback}
            onChange={(v) => setPortDraft(v ?? front.port)}
          />
          {portChanged && front.enabled && (
            <Button
              size="small"
              type="primary"
              loading={save.isPending}
              onClick={() => save.mutate({ enabled: true, frontOnly: false, port })}
            >
              {t('pages.protect.frontPortApply')}
            </Button>
          )}
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            {t('pages.protect.frontPortHint')}
          </Typography.Text>
        </Space>
        <Space wrap>
          <Switch
            checked={front.frontOnly}
            disabled={
              !front.enabled ||
              !front.running.listening ||
              portChanged ||
              (!front.frontOnly && !onFront)
            }
            loading={save.isPending}
            onChange={(on) => save.mutate({ enabled: true, frontOnly: on, port: front.port })}
          />
          <span>{t('pages.protect.frontOnly', { port: front.panelPort })}</span>
        </Space>
        {front.running.listening && !front.frontOnly && !onFront && (
          <Typography.Text type="warning" style={{ fontSize: 12 }}>
            {t('pages.protect.frontOnlyVia443', { url: panelURL })}
          </Typography.Text>
        )}
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          {t('pages.protect.frontOnlyNote')}
        </Typography.Text>
      </Space>
      {front.running.listening && (
        <div style={{ marginTop: 8 }}>
          <Button
            size="small"
            href={`${siteOrigin(publicPort(front))}/`}
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
