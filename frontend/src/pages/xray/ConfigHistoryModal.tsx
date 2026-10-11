import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button, Modal, Popconfirm, Space, Table, Tag, Typography } from 'antd';
import type { TableColumnsType } from 'antd';
import { useQuery } from '@tanstack/react-query';

import { HttpUtil } from '@/utils';

interface Snapshot {
  id: number;
  size: number;
  good: boolean;
  note: string;
  createdAt: number;
}

interface Props {
  open: boolean;
  onClose: () => void;
  onRestored: () => void;
}

export default function ConfigHistoryModal({ open, onClose, onRestored }: Props) {
  const { t } = useTranslation();
  const [viewing, setViewing] = useState<{ id: number; content: string } | null>(null);
  const [restoring, setRestoring] = useState(false);
  const {
    data = [],
    isFetching,
    refetch,
  } = useQuery({
    queryKey: ['xray', 'history'],
    queryFn: async () => {
      const msg = await HttpUtil.get<Snapshot[]>('/panel/api/xray/history/list', undefined, {
        silent: true,
      });
      return msg?.success ? (msg.obj ?? []) : [];
    },
    enabled: open,
    gcTime: 0,
  });

  async function view(id: number) {
    const msg = await HttpUtil.get<{ content: string }>(`/panel/api/xray/history/get/${id}`);
    if (msg?.success && msg.obj) {
      let content = msg.obj.content;
      try {
        content = JSON.stringify(JSON.parse(content), null, 2);
      } catch {
        /* keep the stored text as is */
      }
      setViewing({ id, content });
    }
  }

  async function restore(id: number) {
    setRestoring(true);
    try {
      const msg = await HttpUtil.post(`/panel/api/xray/history/restore/${id}`);
      if (msg?.success) {
        setViewing(null);
        void refetch();
        onRestored();
      }
    } finally {
      setRestoring(false);
    }
  }

  const columns: TableColumnsType<Snapshot> = [
    {
      title: t('pages.xray.history.time'),
      dataIndex: 'createdAt',
      render: (ts: number, row) => (
        <Space size={4} wrap>
          {new Date(ts * 1000).toLocaleString()}
          {row.good && <Tag color="green">{t('pages.xray.history.good')}</Tag>}
          {row.note && <Tag>{row.note}</Tag>}
        </Space>
      ),
    },
    {
      title: t('pages.xray.history.size'),
      dataIndex: 'size',
      width: 90,
      render: (n: number) => `${(n / 1024).toFixed(1)} KB`,
    },
    {
      key: 'actions',
      width: 170,
      render: (_v, row, index) => (
        <Space size={4}>
          <Button size="small" onClick={() => view(row.id)}>
            {t('pages.xray.history.view')}
          </Button>
          {index > 0 && (
            <Popconfirm
              title={t('pages.xray.history.restoreConfirm')}
              onConfirm={() => restore(row.id)}
            >
              <Button size="small" type="primary" loading={restoring}>
                {t('pages.xray.history.restore')}
              </Button>
            </Popconfirm>
          )}
        </Space>
      ),
    },
  ];

  return (
    <Modal
      open={open}
      title={t('pages.xray.history.title')}
      onCancel={onClose}
      footer={null}
      width={760}
      destroyOnHidden
    >
      <Typography.Paragraph type="secondary">{t('pages.xray.history.intro')}</Typography.Paragraph>
      <Table<Snapshot>
        rowKey="id"
        size="small"
        loading={isFetching}
        dataSource={data}
        columns={columns}
        pagination={{ pageSize: 10, hideOnSinglePage: true }}
      />
      <Modal
        open={viewing !== null}
        title={t('pages.xray.history.versionTitle', { id: viewing?.id ?? '' })}
        onCancel={() => setViewing(null)}
        footer={null}
        width={820}
      >
        <pre style={{ maxHeight: '60vh', overflow: 'auto', fontSize: 12 }}>{viewing?.content}</pre>
      </Modal>
    </Modal>
  );
}
