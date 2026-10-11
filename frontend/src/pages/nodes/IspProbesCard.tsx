import { useState } from 'react';
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
}
interface Matrix {
  probes: Probe[];
  targets: Target[];
  results: Result[];
}

const JSON_HEADERS = { headers: { 'Content-Type': 'application/json' } } as const;
const FRESH_SECONDS = 15 * 60;
const ISPS = ['telecom', 'unicom', 'mobile', 'other'];

export default function IspProbesCard() {
  const { t } = useTranslation();
  const [adding, setAdding] = useState(false);
  const [name, setName] = useState('');
  const [isp, setIsp] = useState('telecom');
  const [command, setCommand] = useState('');
  const { data, refetch, isFetching } = useQuery({
    queryKey: ['probes', 'matrix'],
    queryFn: async () => {
      const msg = await HttpUtil.get<Matrix>('/panel/api/probes/matrix', undefined, {
        silent: true,
      });
      const base = msg?.success && msg.obj ? msg.obj : { probes: [], targets: [], results: [] };
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

  async function add() {
    const msg = await HttpUtil.post<{ path: string }>(
      '/panel/api/probes/add',
      { name, isp },
      { ...JSON_HEADERS, silentSuccess: true },
    );
    if (msg?.success && msg.obj) {
      const url = `${window.location.origin}${msg.obj.path}`;
      setCommand(`curl -fsSL '${url}/agent.sh' | sh -s -- install '${url}'`);
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
            {row.node} · {row.name}
          </span>
        </Tooltip>
      ),
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
              icon={<DeleteOutlined />}
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
        return r.ok ? (
          <Tag color="green">{r.latencyMs} ms</Tag>
        ) : (
          <Tag color="red">{t('pages.nodes.probes.blocked')}</Tag>
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
          <RadarChartOutlined />
          {t('pages.nodes.probes.title')}
        </Space>
      }
      extra={
        <Button
          size="small"
          icon={<PlusOutlined />}
          onClick={() => {
            setName('');
            setCommand('');
            setAdding(true);
          }}
        >
          {t('pages.nodes.probes.add')}
        </Button>
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
          </>
        ) : (
          <Form layout="vertical">
            <Typography.Paragraph type="secondary">
              {t('pages.nodes.probes.intro')}
            </Typography.Paragraph>
            <Form.Item label={t('pages.nodes.probes.name')}>
              <Input
                value={name}
                placeholder={t('pages.nodes.probes.namePlaceholder')}
                onChange={(e) => setName(e.target.value)}
              />
            </Form.Item>
            <Form.Item label={t('pages.nodes.probes.isp')}>
              <Select
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
