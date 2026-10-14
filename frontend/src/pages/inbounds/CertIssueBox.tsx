import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Alert, Button, Checkbox, Input, Space, Typography } from 'antd';

import { HttpUtil } from '@/utils';
import type { CertIssueStatus } from '@/generated/types';

const JSON_HEADERS = { headers: { 'Content-Type': 'application/json' } } as const;
const POLL_MS = 3000;

interface CertIssueBoxProps {
  domain: string;
  // Called once the certificate is installed, so the caller can look at the server again.
  onIssued: () => void;
}

async function fetchStatus(): Promise<CertIssueStatus | null> {
  const msg = await HttpUtil.get<CertIssueStatus>('/panel/api/protect/cert/issue', undefined, {
    silent: true,
  });
  return msg?.success ? (msg.obj ?? null) : null;
}

// Gets a Let's Encrypt certificate through Cloudflare DNS, so a server without one can still take
// the TLS presets; nothing listens on port 80 for it.
export default function CertIssueBox({ domain: initialDomain, onIssued }: CertIssueBoxProps) {
  const { t } = useTranslation();
  const [domain, setDomain] = useState(initialDomain);
  const [usePanel, setUsePanel] = useState(false);
  const [status, setStatus] = useState<CertIssueStatus | null>(null);
  const [starting, setStarting] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(
    () => () => {
      if (timer.current) clearTimeout(timer.current);
    },
    [],
  );

  const poll = async () => {
    const st = await fetchStatus();
    if (st) setStatus(st);
    if (st?.running) {
      timer.current = setTimeout(() => void poll(), POLL_MS);
      return;
    }
    if (st?.done) onIssued();
  };

  const start = async () => {
    setStarting(true);
    try {
      const msg = await HttpUtil.post<CertIssueStatus>(
        '/panel/api/protect/cert/issue',
        { domain: domain.trim(), usePanel },
        { ...JSON_HEADERS, silentSuccess: true },
      );
      if (msg?.success) {
        setStatus(msg.obj ?? null);
        timer.current = setTimeout(() => void poll(), POLL_MS);
      }
    } finally {
      setStarting(false);
    }
  };

  const running = !!status?.running;
  const panelUrl = `https://${status?.domain || domain}${window.location.port ? `:${window.location.port}` : ''}${window.location.pathname}`;

  return (
    <div className="quick-inbound-cert">
      <Typography.Text strong>{t('pages.inbounds.quick.certIssueTitle')}</Typography.Text>
      <Typography.Paragraph type="secondary" className="quick-inbound-desc">
        {t('pages.inbounds.quick.certIssueDesc')}
      </Typography.Paragraph>
      <Space wrap size={8}>
        <Input
          size="small"
          value={domain}
          disabled={running}
          placeholder="hk.example.com"
          onChange={(e) => setDomain(e.target.value)}
        />
        <Checkbox
          checked={usePanel}
          disabled={running}
          onChange={(e) => setUsePanel(e.target.checked)}
        >
          {t('pages.inbounds.quick.certIssuePanel')}
        </Checkbox>
        <Button
          size="small"
          type="primary"
          loading={starting || running}
          disabled={!domain.trim()}
          onClick={() => void start()}
        >
          {t('pages.inbounds.quick.certIssueStart')}
        </Button>
      </Space>
      {usePanel && !running && (
        <Typography.Paragraph type="warning" className="quick-inbound-desc">
          {t('pages.inbounds.quick.certIssuePanelNote', { url: panelUrl })}
        </Typography.Paragraph>
      )}
      {running && (
        <Typography.Paragraph type="secondary" className="quick-inbound-desc">
          {t(`pages.inbounds.quick.certIssueStep.${status?.step || 'issue'}`)}
        </Typography.Paragraph>
      )}
      {status?.done && (
        <Alert
          type="success"
          showIcon
          title={
            status.panel
              ? t('pages.inbounds.quick.certIssueDonePanel', { url: panelUrl })
              : t('pages.inbounds.quick.certIssueDone', { domain: status.domain })
          }
        />
      )}
      {status?.error && !running && (
        <Alert
          type="error"
          showIcon
          title={status.error}
          description={status.log ? <pre className="quick-inbound-log">{status.log}</pre> : null}
        />
      )}
    </div>
  );
}
