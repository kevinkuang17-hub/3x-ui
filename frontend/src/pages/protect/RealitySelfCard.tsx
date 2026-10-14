import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Alert, Button, Card, Select, Space, Steps, Typography } from 'antd';
import { SafetyCertificateOutlined } from '@ant-design/icons';
import { useQuery, useQueryClient } from '@tanstack/react-query';

import { HttpUtil } from '@/utils';
import { keys } from '@/api/queryKeys';
import { coerceInboundJsonField } from '@/models/dbinbound';
import { frontKey, useFront, type FrontView } from './FrontSection';

const API = '/panel/api/protect';
const JSON_HEADERS = { headers: { 'Content-Type': 'application/json' } } as const;

interface SlimInbound {
  id: number;
  remark: string;
  port: number;
  protocol: string;
  nodeId?: number | null;
  streamSettings?: unknown;
}

interface Plan {
  domain: string;
  internalPort: number;
}

const sleep = (ms: number) => new Promise((resolve) => window.setTimeout(resolve, ms));

// waitForWebsite polls until the restarted panel runs the website on its loopback port.
export async function waitForWebsite(port: number, tries = 30, gapMs = 2000): Promise<boolean> {
  for (let i = 0; i < tries; i++) {
    await sleep(gapMs);
    try {
      const msg = await HttpUtil.get<FrontView>(`${API}/front`, undefined, { silent: true });
      const front = msg?.obj;
      if (front?.loopback && front.running.listening && front.running.addr?.endsWith(`:${port}`))
        return true;
    } catch {
      /* the panel is restarting; try again */
    }
  }
  return false;
}

// RealitySelfCard puts a REALITY inbound on 443 in front of the panel's own website: the SNI is the
// server's domain, so what anyone sees on 443 is a real site with a valid certificate on that IP.
export default function RealitySelfCard() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const front = useFront().data;
  const inbounds = useQuery({
    queryKey: [...keys.protect.root(), 'realityInbounds'],
    queryFn: async () => {
      const msg = await HttpUtil.get<SlimInbound[]>('/panel/api/inbounds/list/slim', undefined, {
        silent: true,
      });
      return (msg?.obj ?? []).filter((ib) => {
        const stream = coerceInboundJsonField(ib.streamSettings);
        return ib.nodeId == null && ib.protocol === 'vless' && stream.security === 'reality';
      });
    },
  });
  const [inboundId, setInboundId] = useState<number | null>(null);
  const [step, setStep] = useState(-1);
  const [error, setError] = useState('');
  const [done, setDone] = useState<Plan | null>(null);
  const chosen = inboundId ?? inbounds.data?.[0]?.id ?? null;
  const active = !!front?.loopback && !!front.running.listening;

  async function run() {
    if (chosen == null) return;
    setError('');
    setDone(null);
    setStep(0);
    const prep = await HttpUtil.post<Plan>(
      `${API}/realitySelf/prepare`,
      { inboundId: chosen },
      JSON_HEADERS,
    );
    if (!prep?.success || !prep.obj) {
      setError(prep?.msg || t('somethingWentWrong'));
      setStep(-1);
      return;
    }
    const plan = prep.obj;
    setStep(1);
    if (!(await waitForWebsite(plan.internalPort))) {
      setError(t('pages.protect.realitySelfTimeout'));
      setStep(-1);
      return;
    }
    setStep(2);
    const applied = await HttpUtil.post(
      `${API}/realitySelf/apply`,
      { inboundId: chosen },
      JSON_HEADERS,
    );
    if (!applied?.success) {
      setError(applied?.msg || t('somethingWentWrong'));
      setStep(-1);
      return;
    }
    setStep(3);
    setDone(plan);
    void queryClient.invalidateQueries({ queryKey: frontKey() });
    void inbounds.refetch();
  }

  const running = step >= 0 && step < 3;
  return (
    <Card size="small" title={t('pages.protect.realitySelfTitle')}>
      <Typography.Paragraph type="secondary">
        {t('pages.protect.realitySelfDesc')}
      </Typography.Paragraph>
      {active && !done && (
        <Alert
          type="success"
          showIcon
          style={{ marginBottom: 12 }}
          title={t('pages.protect.realitySelfActive', { addr: front?.running.addr ?? '' })}
        />
      )}
      {done && (
        <Alert
          type="success"
          showIcon
          style={{ marginBottom: 12 }}
          title={t('pages.protect.realitySelfDone', { domain: done.domain })}
          description={t('pages.protect.realitySelfUpdateSubs')}
        />
      )}
      {error && <Alert type="error" showIcon style={{ marginBottom: 12 }} title={error} />}
      {inbounds.data && inbounds.data.length === 0 ? (
        <Alert type="info" showIcon title={t('pages.protect.realitySelfNoInbound')} />
      ) : (
        <Space orientation="vertical" size={12} style={{ width: '100%' }}>
          <Space wrap>
            <Select
              style={{ minWidth: 240 }}
              value={chosen ?? undefined}
              onChange={setInboundId}
              options={(inbounds.data ?? []).map((ib) => ({
                value: ib.id,
                label: `${ib.remark || `#${ib.id}`} (${ib.port})`,
              }))}
            />
            <Button
              type="primary"
              icon={<SafetyCertificateOutlined />}
              loading={running}
              disabled={chosen == null}
              onClick={() => void run()}
            >
              {t('pages.protect.realitySelfRun')}
            </Button>
          </Space>
          {step >= 0 && (
            <Steps
              size="small"
              current={step}
              items={[
                { title: t('pages.protect.realitySelfStepMove') },
                { title: t('pages.protect.realitySelfStepRestart') },
                { title: t('pages.protect.realitySelfStepReality') },
              ]}
            />
          )}
        </Space>
      )}
    </Card>
  );
}
