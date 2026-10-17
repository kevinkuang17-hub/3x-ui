import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router';
import { Alert, Button, Card, Empty, Modal, Space, Spin, Tag, Typography } from 'antd';
import {
  CheckCircleFilled,
  CloseCircleFilled,
  ExclamationCircleFilled,
  ReloadOutlined,
} from '@ant-design/icons';

import { HttpUtil } from '@/utils';
import { chinaCheckLinks } from '@/lib/china-check';

import type { ChinaVerdict } from '@/pages/nodes/IspProbesCard';
import type { Finding, Report } from '@/generated/types';

import './DiagnoseModal.css';

const JSON_HEADERS = { headers: { 'Content-Type': 'application/json' } } as const;

interface DiagnoseModalProps {
  open: boolean;
  // Inbounds to check; empty checks every inbound.
  ids: number[];
  onClose: () => void;
  onChanged: () => void;
}

async function runDiagnose(ids: number[]): Promise<Report[]> {
  const msg = await HttpUtil.post<Report[]>('/panel/api/inbounds/diagnose', { ids }, JSON_HEADERS);
  if (!msg?.success) throw new Error(msg?.msg || 'diagnose');
  return msg.obj ?? [];
}

const LEVEL_ICON: Record<string, React.ReactNode> = {
  ok: <CheckCircleFilled className="diag-ok" />,
  warn: <ExclamationCircleFilled className="diag-warn" />,
  error: <CloseCircleFilled className="diag-error" />,
};

// Problems first, so a long list starts with what needs attention.
function sortReports(list: Report[]): Report[] {
  return [...list].sort((a, b) => Number(a.ok) - Number(b.ok) || a.inboundId - b.inboundId);
}

export default function DiagnoseModal({ open, ids, onClose, onChanged }: DiagnoseModalProps) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [reports, setReports] = useState<Report[] | null>(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [fixing, setFixing] = useState<string>('');
  const [probeVerdicts, setProbeVerdicts] = useState<Record<string, ChinaVerdict>>({});
  const [probeCount, setProbeCount] = useState(0);

  const run = useCallback(async (only: number[]) => {
    setBusy(true);
    setError('');
    try {
      const fresh = await runDiagnose(only);
      setReports((prev) => {
        if (!prev || only.length === 0) return sortReports(fresh);
        const byId = new Map(fresh.map((r) => [r.inboundId, r]));
        return sortReports(prev.map((r) => byId.get(r.inboundId) ?? r));
      });
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  }, []);

  useEffect(() => {
    if (!open) return;
    setReports(null);
    void run(ids);
    // What the ISP probes inside China last saw, when any are installed.
    void HttpUtil.get<{ verdicts?: Record<string, ChinaVerdict>; probes?: unknown[] }>(
      '/panel/api/probes/matrix',
      undefined,
      { silent: true },
    ).then((msg) => {
      setProbeVerdicts(msg?.success ? (msg.obj?.verdicts ?? {}) : {});
      setProbeCount(msg?.success ? (msg.obj?.probes?.length ?? 0) : 0);
    });
    /* eslint-disable-next-line react-hooks/exhaustive-deps */
  }, [open]);

  async function fix(report: Report, finding: Finding) {
    const key = `${report.inboundId}:${finding.key}`;
    setFixing(key);
    try {
      let ok = false;
      if (finding.fix === 'enable') {
        const form = new FormData();
        form.append('enable', 'true');
        ok = !!(await HttpUtil.post(`/panel/api/inbounds/setEnable/${report.inboundId}`, form))
          ?.success;
      } else if (finding.fix === 'firewall') {
        const url =
          report.nodeId != null
            ? `/panel/api/protect/nodes/${report.nodeId}/firewallAllow`
            : '/panel/api/protect/firewall/allow';
        ok = !!(await HttpUtil.post(url, { rules: [] }, JSON_HEADERS))?.success;
      } else if (finding.fix === 'clients') {
        onClose();
        navigate('/clients');
        return;
      }
      if (ok) {
        onChanged();
        await run([report.inboundId]);
      }
    } finally {
      setFixing('');
    }
  }

  const findingText = (f: Finding) =>
    t(`pages.inbounds.diagnose.find.${f.key}`, (f.params ?? {}) as Record<string, unknown>);
  const adviceText = (f: Finding) => {
    const key = `pages.inbounds.diagnose.advice.${f.key}`;
    const text = t(key, (f.params ?? {}) as Record<string, unknown>);
    return text === key ? '' : text;
  };

  // The checks above run from this panel's own server abroad; this row is about China.
  // ITDOG refuses to be framed (X-Frame-Options), so its test opens in a new tab.
  const renderChinaCheck = (r: Report) => {
    const links = chinaCheckLinks(r.address, r.port, r.protocol);
    if (!links) return null;
    const verdict = probeVerdicts[String(r.inboundId)];
    return (
      <div className="diag-china">
        {verdict ? (
          <Tag
            color={verdict.state === 'ok' ? 'green' : verdict.state === 'port' ? 'orange' : 'red'}
          >
            {t('pages.inbounds.diagnose.china.probes', {
              state: t(`pages.nodes.probes.verdictState.${verdict.state}`),
              ok: verdict.tcpOk,
              total: verdict.probes,
            })}
          </Tag>
        ) : (
          <span className="diag-china-unknown">
            {LEVEL_ICON.warn}{' '}
            {t(
              probeCount > 0
                ? 'pages.inbounds.diagnose.china.noResult'
                : 'pages.inbounds.diagnose.china.unknown',
            )}
          </span>
        )}
        <Space wrap size={6}>
          <Button size="small" href={links.ping} target="_blank" rel="noopener noreferrer">
            {t('pages.inbounds.diagnose.china.ping')}
          </Button>
          {links.tcping ? (
            <Button size="small" href={links.tcping} target="_blank" rel="noopener noreferrer">
              {t('pages.inbounds.diagnose.china.tcping', { port: r.port })}
            </Button>
          ) : (
            <Typography.Text type="secondary">
              {t('pages.inbounds.diagnose.china.udpOnly')}
            </Typography.Text>
          )}
          {probeCount === 0 && (
            <Button
              size="small"
              type="link"
              onClick={() => {
                onClose();
                navigate('/nodes');
              }}
            >
              {t('pages.inbounds.diagnose.china.addProbe')}
            </Button>
          )}
        </Space>
      </div>
    );
  };

  const problems = reports?.filter((r) => !r.ok).length ?? 0;

  return (
    <Modal
      open={open}
      width={820}
      title={t('pages.inbounds.diagnose.title')}
      onCancel={onClose}
      destroyOnHidden
      footer={[
        <Button key="again" icon={<ReloadOutlined />} disabled={busy} onClick={() => void run(ids)}>
          {t('pages.inbounds.diagnose.again')}
        </Button>,
        <Button key="close" type="primary" onClick={onClose}>
          {t('close')}
        </Button>,
      ]}
    >
      <Space orientation="vertical" size={12} style={{ width: '100%' }}>
        {busy && (
          <Space>
            <Spin size="small" />
            <Typography.Text type="secondary">
              {t('pages.inbounds.diagnose.running')}
            </Typography.Text>
          </Space>
        )}
        {error && <Alert type="error" showIcon title={error} />}
        {reports && !busy && (
          <Alert
            type={problems > 0 ? 'warning' : 'success'}
            showIcon
            title={
              problems > 0
                ? t('pages.inbounds.diagnose.summaryBad', {
                    count: problems,
                    total: reports.length,
                  })
                : t('pages.inbounds.diagnose.summaryGood', { total: reports.length })
            }
          />
        )}
        {reports && reports.length === 0 && <Empty />}
        {reports?.map((r) => (
          <Card
            key={r.inboundId}
            size="small"
            className={r.ok ? 'diag-card' : 'diag-card is-bad'}
            title={
              <Space wrap size={6}>
                {r.ok ? LEVEL_ICON.ok : LEVEL_ICON.error}
                <Typography.Text strong>{r.remark || `#${r.inboundId}`}</Typography.Text>
                <Tag>
                  {r.protocol}:{r.port}
                </Tag>
                {r.address && <Typography.Text type="secondary">{r.address}</Typography.Text>}
              </Space>
            }
            extra={
              r.ok ? (
                <Tag color="success">
                  {t('pages.inbounds.diagnose.okDelay', { delay: r.delayMs })}
                </Tag>
              ) : (
                <Tag color="error">{t('pages.inbounds.diagnose.bad')}</Tag>
              )
            }
          >
            <div className="diag-findings">
              {r.findings.map((f) => (
                <div key={f.key} className={`diag-finding level-${f.level}`} data-key={f.key}>
                  <span className="diag-icon">{LEVEL_ICON[f.level]}</span>
                  <div className="diag-body">
                    <Typography.Text>{findingText(f)}</Typography.Text>
                    {f.level !== 'ok' && adviceText(f) && (
                      <Typography.Paragraph type="secondary" className="diag-advice">
                        {adviceText(f)}
                      </Typography.Paragraph>
                    )}
                    {f.fix && (
                      <Button
                        size="small"
                        type="primary"
                        loading={fixing === `${r.inboundId}:${f.key}`}
                        onClick={() => void fix(r, f)}
                      >
                        {t(`pages.inbounds.diagnose.fixAction.${f.fix}`)}
                      </Button>
                    )}
                  </div>
                </div>
              ))}
            </div>
            {renderChinaCheck(r)}
          </Card>
        ))}
        <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginBottom: 0 }}>
          {t('pages.inbounds.diagnose.note')}
        </Typography.Paragraph>
        <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginBottom: 0 }}>
          {t('pages.inbounds.diagnose.china.hint')}
        </Typography.Paragraph>
      </Space>
    </Modal>
  );
}
