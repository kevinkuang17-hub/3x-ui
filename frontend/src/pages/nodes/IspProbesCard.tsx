import { useId, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Button,
  Card,
  Form,
  Input,
  Modal,
  Popconfirm,
  Select,
  Space,
  Table,
  Tag,
  Tooltip,
  Typography,
} from 'antd';
import type { TableColumnsType } from 'antd';
import { DeleteOutlined, PlusOutlined, RadarChartOutlined } from '@ant-design/icons';
import { useQuery } from '@tanstack/react-query';

import { HttpUtil } from '@/utils';

interface Probe {
  id: number;
  name: string;
  isp: string;
  lastSeen: number;
  lastIp: string;
}
interface Target {
  inboundId: number;
  name: string;
  node: string;
  host: string;
  port: number;
}
interface Result {
  probeId: number;
  inboundId: number;
  ok: boolean;
  latencyMs: number;
  checkedAt: number;
  pingMs: number;
  proxied: boolean;
}
export interface ChinaVerdict {
  state: 'ok' | 'port' | 'ip' | 'blocked';
  probes: number;
  tcpOk: number;
  pingOk: number;
}
interface Matrix {
  probes: Probe[];
  targets: Target[];
  results: Result[];
  verdicts?: Record<string, ChinaVerdict>;
}

const VERDICT_COLOR: Record<ChinaVerdict['state'], string> = {
  ok: 'green',
  port: 'orange',
  ip: 'red',
  blocked: 'red',
};

const JSON_HEADERS = { headers: { 'Content-Type': 'application/json' } } as const;
const FRESH_SECONDS = 15 * 60;
const ISPS = ['telecom', 'unicom', 'mobile', 'other'];

export default function IspProbesCard() {
  const uid = useId();
  const { t } = useTranslation();
  const [adding, setAdding] = useState(false);
  const [name, setName] = useState('');
  const [isp, setIsp] = useState('telecom');
  const [command, setCommand] = useState('');
  const [altCommand, setAltCommand] = useState('');
  const [enroll, setEnroll] = useState<{ command: string; alt: string } | null>(null);

  async function loadEnroll(reset: boolean) {
    const msg = reset
      ? await HttpUtil.post<{ path: string; subUrl?: string }>(
          '/panel/api/probes/enroll/reset',
          {},
          { ...JSON_HEADERS, silentSuccess: true },
        )
      : await HttpUtil.get<{ path: string; subUrl?: string }>('/panel/api/probes/enroll');
    if (!msg?.success || !msg.obj) return;
    const panelUrl = `${window.location.origin}${msg.obj.path}`;
    const cmd = (url: string) => `curl -fsSL '${url}/agent.sh' | sh -s -- enroll '${url}'`;
    setEnroll({
      command: cmd(msg.obj.subUrl || panelUrl),
      alt: msg.obj.subUrl ? cmd(panelUrl) : '',
    });
  }
  const { data, refetch, isFetching } = useQuery({
    queryKey: ['probes', 'matrix'],
    queryFn: async () => {
      const msg = await HttpUtil.get<Matrix>('/panel/api/probes/matrix', undefined, {
        silent: true,
      });
      const base =
        msg?.success && msg.obj ? msg.obj : { probes: [], targets: [], results: [], verdicts: {} };
      return { ...base, fetchedAt: Date.now() / 1000 };
    },
    refetchInterval: 60_000,
  });
  const probes = data?.probes ?? [];
  const targets = data?.targets ?? [];
  const results = new Map(
    (data?.results ?? []).map((r) => [`${r.probeId}:${r.inboundId}`, r] as const),
  );
  const now = data?.fetchedAt ?? 0;
  const verdicts = data?.verdicts ?? {};

  async function add() {
    const msg = await HttpUtil.post<{ path: string; subUrl?: string }>(
      '/panel/api/probes/add',
      { name, isp },
      { ...JSON_HEADERS, silentSuccess: true },
    );
    if (msg?.success && msg.obj) {
      const panelUrl = `${window.location.origin}${msg.obj.path}`;
      const install = (url: string) => `curl -fsSL '${url}/agent.sh' | sh -s -- install '${url}'`;
      // The subscription domain usually stays reachable from China when the panel domain does not.
      setCommand(install(msg.obj.subUrl || panelUrl));
      setAltCommand(msg.obj.subUrl ? install(panelUrl) : '');
      void refetch();
    }
  }

  const columns: TableColumnsType<Target> = [
    {
      title: t('pages.nodes.probes.line'),
      key: 'line',
      fixed: 'left',
      render: (_v, row) => (
        <Tooltip title={`${row.host}:${row.port}`}>
          <span>
            {row.node || t('pages.inbounds.localPanel')} · {row.name}
          </span>
        </Tooltip>
      ),
    },
    {
      title: t('pages.nodes.probes.verdict'),
      key: 'verdict',
      render: (_v, row) => {
        const v = verdicts[String(row.inboundId)];
        if (!v) return <Tag>{t('pages.nodes.probes.noData')}</Tag>;
        return (
          <Tooltip title={t(`pages.nodes.probes.verdictHint.${v.state}`)}>
            <Tag color={VERDICT_COLOR[v.state]}>
              {t(`pages.nodes.probes.verdictState.${v.state}`)}
            </Tag>
          </Tooltip>
        );
      },
    },
    ...probes.map((p) => ({
      title: (
        <Space size={4}>
          {p.name}
          <Tag>{t(`pages.nodes.probes.isp_${ISPS.includes(p.isp) ? p.isp : 'other'}`)}</Tag>
          <Popconfirm
            title={t('pages.nodes.probes.deleteConfirm', { name: p.name })}
            onConfirm={async () => {
              const msg = await HttpUtil.post(`/panel/api/probes/del/${p.id}`);
              if (msg?.success) void refetch();
            }}
          >
            <Button
              size="small"
              type="text"
              danger
              icon={<DeleteOutlined aria-hidden />}
              aria-label={t('delete')}
            />
          </Popconfirm>
        </Space>
      ),
      key: `p${p.id}`,
      render: (_v: unknown, row: Target) => {
        const r = results.get(`${p.id}:${row.inboundId}`);
        if (!r || now - r.checkedAt > FRESH_SECONDS)
          return <Tag>{t('pages.nodes.probes.noData')}</Tag>;
        if (r.proxied)
          return (
            <Tooltip title={t('pages.nodes.probes.proxiedHint')}>
              <Tag color="gold">{t('pages.nodes.probes.proxied')}</Tag>
            </Tooltip>
          );
        const ping =
          r.pingMs > 0
            ? t('pages.nodes.probes.pingMs', { ms: r.pingMs })
            : r.pingMs < 0
              ? t('pages.nodes.probes.pingLost')
              : '';
        return (
          <Space size={2} direction="vertical">
            {r.ok ? (
              <Tag color="green">{r.latencyMs} ms</Tag>
            ) : (
              <Tag color="red">{t('pages.nodes.probes.blocked')}</Tag>
            )}
            {ping && (
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                {ping}
              </Typography.Text>
            )}
          </Space>
        );
      },
    })),
  ];

  return (
    <Card
      size="small"
      hoverable
      title={
        <Space>
          <RadarChartOutlined aria-hidden />
          {t('pages.nodes.probes.title')}
        </Space>
      }
      extra={
        <Space size={6}>
          <Button
            size="small"
            type="primary"
            icon={<PlusOutlined aria-hidden />}
            onClick={() => void loadEnroll(false)}
          >
            {t('pages.nodes.probes.enroll')}
          </Button>
          <Button
            size="small"
            onClick={() => {
              setName('');
              setCommand('');
              setAdding(true);
            }}
          >
            {t('pages.nodes.probes.add')}
          </Button>
        </Space>
      }
    >
      {probes.length === 0 ? (
        <Typography.Paragraph type="secondary" style={{ marginBottom: 0 }}>
          {t('pages.nodes.probes.intro')}
        </Typography.Paragraph>
      ) : (
        <Table<Target>
          rowKey="inboundId"
          size="small"
          loading={isFetching}
          dataSource={targets}
          columns={columns}
          pagination={false}
          scroll={{ x: 'max-content' }}
        />
      )}
      <Modal
        open={!!enroll}
        title={t('pages.nodes.probes.enroll')}
        onCancel={() => setEnroll(null)}
        footer={[
          <Popconfirm
            key="reset"
            title={t('pages.nodes.probes.enrollResetConfirm')}
            onConfirm={() => void loadEnroll(true)}
          >
            <Button danger>{t('pages.nodes.probes.enrollReset')}</Button>
          </Popconfirm>,
          <Button key="close" type="primary" onClick={() => setEnroll(null)}>
            {t('close')}
          </Button>,
        ]}
        destroyOnHidden
      >
        <Typography.Paragraph>{t('pages.nodes.probes.enrollHint')}</Typography.Paragraph>
        <Typography.Paragraph
          code
          copyable={{ text: enroll?.command }}
          style={{ wordBreak: 'break-all' }}
        >
          {enroll?.command}
        </Typography.Paragraph>
        {enroll?.alt && (
          <>
            <Typography.Paragraph type="secondary">
              {t('pages.nodes.probes.altCommand')}
            </Typography.Paragraph>
            <Typography.Paragraph
              code
              copyable={{ text: enroll.alt }}
              style={{ wordBreak: 'break-all' }}
            >
              {enroll.alt}
            </Typography.Paragraph>
          </>
        )}
        <Typography.Paragraph type="secondary">
          {t('pages.nodes.probes.openclashHint')}
        </Typography.Paragraph>
      </Modal>
      <Modal
        open={adding}
        title={t('pages.nodes.probes.add')}
        onCancel={() => setAdding(false)}
        footer={
          command ? (
            <Button onClick={() => setAdding(false)}>{t('close')}</Button>
          ) : (
            <Button type="primary" disabled={!name.trim()} onClick={add}>
              {t('create')}
            </Button>
          )
        }
        destroyOnHidden
      >
        {command ? (
          <>
            <Typography.Paragraph>{t('pages.nodes.probes.installHint')}</Typography.Paragraph>
            <Typography.Paragraph
              code
              copyable={{ text: command }}
              style={{ wordBreak: 'break-all' }}
            >
              {command}
            </Typography.Paragraph>
            {altCommand && (
              <>
                <Typography.Paragraph type="secondary">
                  {t('pages.nodes.probes.altCommand')}
                </Typography.Paragraph>
                <Typography.Paragraph
                  code
                  copyable={{ text: altCommand }}
                  style={{ wordBreak: 'break-all' }}
                >
                  {altCommand}
                </Typography.Paragraph>
              </>
            )}
            <Typography.Paragraph type="secondary">
              {t('pages.nodes.probes.openclashHint')}
            </Typography.Paragraph>
          </>
        ) : (
          <Form layout="vertical">
            <Typography.Paragraph type="secondary">
              {t('pages.nodes.probes.intro')}
            </Typography.Paragraph>
            <Form.Item htmlFor={`${uid}-input-357`} label={t('pages.nodes.probes.name')}>
              <Input
                id={`${uid}-input-357`}
                value={name}
                placeholder={t('pages.nodes.probes.namePlaceholder')}
                onChange={(e) => setName(e.target.value)}
              />
            </Form.Item>
            <Form.Item htmlFor={`${uid}-select-364`} label={t('pages.nodes.probes.isp')}>
              <Select
                id={`${uid}-select-364`}
                value={isp}
                onChange={setIsp}
                options={ISPS.map((v) => ({ value: v, label: t(`pages.nodes.probes.isp_${v}`) }))}
              />
            </Form.Item>
          </Form>
        )}
      </Modal>
    </Card>
  );
}
