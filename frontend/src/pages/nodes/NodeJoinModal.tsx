import { useTranslation } from 'react-i18next';
import { Alert, Button, Modal, Space, Spin, Typography } from 'antd';
import { CopyOutlined } from '@ant-design/icons';
import { useQuery } from '@tanstack/react-query';

import { ClipboardManager, HttpUtil } from '@/utils';

interface Props {
  open: boolean;
  onClose: () => void;
  onCopied: () => void;
}

interface Ticket {
  token: string;
  expiresAt: number;
  path: string;
  script: string;
}

export default function NodeJoinModal({ open, onClose, onCopied }: Props) {
  const { t } = useTranslation();
  const { data: ticket = null, isFetching: loading } = useQuery({
    queryKey: ['nodes', 'joinTicket'],
    queryFn: async () => {
      const msg = await HttpUtil.post<Ticket>('/panel/api/nodes/join/ticket', undefined, {
        silentSuccess: true,
      });
      return msg?.success && msg.obj ? msg.obj : null;
    },
    enabled: open,
    gcTime: 0,
    staleTime: Infinity,
    refetchOnWindowFocus: false,
  });

  const command = ticket
    ? `bash <(curl -Ls ${ticket.script}) '${window.location.origin}${ticket.path}'`
    : '';

  return (
    <Modal
      open={open}
      title={t('pages.nodes.join.title')}
      onCancel={onClose}
      footer={<Button onClick={onClose}>{t('close')}</Button>}
      destroyOnHidden
      width={720}
    >
      <Typography.Paragraph>{t('pages.nodes.join.intro')}</Typography.Paragraph>
      <Spin spinning={loading}>
        {command && (
          <Space orientation="vertical" style={{ width: '100%' }}>
            <Typography.Paragraph
              code
              style={{ wordBreak: 'break-all', fontFamily: 'monospace', marginBottom: 0 }}
            >
              {command}
            </Typography.Paragraph>
            <Button
              type="primary"
              icon={<CopyOutlined aria-hidden />}
              onClick={async () => {
                if (await ClipboardManager.copyText(command)) onCopied();
              }}
            >
              {t('copy')}
            </Button>
            <Typography.Text type="secondary">
              {t('pages.nodes.join.expires', {
                time: new Date((ticket?.expiresAt ?? 0) * 1000).toLocaleString(),
              })}
            </Typography.Text>
          </Space>
        )}
      </Spin>
      <Alert type="info" showIcon style={{ marginTop: 16 }} title={t('pages.nodes.join.notes')} />
    </Modal>
  );
}
