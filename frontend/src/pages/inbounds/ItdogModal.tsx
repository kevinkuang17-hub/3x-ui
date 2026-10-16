import { useTranslation } from 'react-i18next';
import { Button, Modal, Space, Typography } from 'antd';
import { ExportOutlined } from '@ant-design/icons';

interface ItdogModalProps {
  // The ITDOG test page to show; empty closes the dialog.
  url: string;
  title: string;
  onClose: () => void;
}

// Shows ITDOG's test (map, provinces, carriers) inside the panel. ITDOG has no data API, so its
// own page is embedded; the open-in-tab link covers a browser that refuses to frame it.
export default function ItdogModal({ url, title, onClose }: ItdogModalProps) {
  const { t } = useTranslation();
  return (
    <Modal
      open={!!url}
      title={title}
      width="min(1280px, 96vw)"
      style={{ top: 16 }}
      footer={null}
      onCancel={onClose}
      destroyOnHidden
    >
      <Space className="itdog-bar" wrap>
        <Typography.Text type="secondary">
          {t('pages.inbounds.diagnose.china.embedHint')}
        </Typography.Text>
        <Button
          size="small"
          icon={<ExportOutlined />}
          href={url}
          target="_blank"
          rel="noopener noreferrer"
        >
          {t('pages.inbounds.diagnose.china.openTab')}
        </Button>
      </Space>
      {url && (
        <iframe
          className="itdog-frame"
          src={url}
          title={title}
          referrerPolicy="no-referrer"
          sandbox="allow-scripts allow-same-origin allow-forms allow-popups"
        />
      )}
    </Modal>
  );
}
