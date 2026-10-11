import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Alert, Form, Input, Modal, Select } from 'antd';

import { CLASH_ROUTING_PRESETS, presetOf } from '@/pages/settings/clashRoutingPresets';

const FOLLOW_GLOBAL = '__global__';
const CUSTOM = '__custom__';

interface Props {
  open: boolean;
  groupName: string | null;
  initialRules: string;
  saving: boolean;
  onClose: () => void;
  onSubmit: (rules: string) => Promise<boolean>;
}

function choiceOf(rules: string): string {
  if (!rules.trim()) return FOLLOW_GLOBAL;
  return presetOf(rules)?.key ?? CUSTOM;
}

export default function GroupClashRulesModal({
  open,
  groupName,
  initialRules,
  saving,
  onClose,
  onSubmit,
}: Props) {
  const { t } = useTranslation();
  const [choice, setChoice] = useState(() => choiceOf(initialRules));
  const [custom, setCustom] = useState(() =>
    choiceOf(initialRules) === CUSTOM ? initialRules : '',
  );

  async function confirm() {
    let rules = '';
    if (choice === CUSTOM) rules = custom.trim();
    else if (choice !== FOLLOW_GLOBAL) {
      rules = CLASH_ROUTING_PRESETS.find((p) => p.key === choice)?.url ?? '';
    }
    if (await onSubmit(rules)) onClose();
  }

  return (
    <Modal
      open={open}
      title={t('pages.groups.clashRulesTitle', { name: groupName ?? '' })}
      okText={t('save')}
      cancelText={t('cancel')}
      confirmLoading={saving}
      onCancel={onClose}
      onOk={confirm}
      destroyOnHidden
    >
      <Alert
        type="info"
        showIcon
        title={t('pages.groups.clashRulesDesc')}
        style={{ marginBottom: 16 }}
      />
      <Form layout="vertical">
        <Form.Item label={t('pages.settings.subClashPreset')}>
          <Select
            value={choice}
            onChange={setChoice}
            options={[
              { value: FOLLOW_GLOBAL, label: t('pages.groups.clashRulesFollowGlobal') },
              ...CLASH_ROUTING_PRESETS.map((p) => ({
                value: p.key,
                label: t(`pages.settings.subClashPreset_${p.key}`),
              })),
              { value: CUSTOM, label: t('pages.groups.clashRulesCustom') },
            ]}
          />
        </Form.Item>
        {choice === CUSTOM && (
          <Form.Item label={t('pages.groups.clashRulesCustom')}>
            <Input.TextArea
              value={custom}
              onChange={(e) => setCustom(e.target.value)}
              autoSize={{ minRows: 3, maxRows: 10 }}
              placeholder={t('pages.groups.clashRulesCustomPlaceholder')}
            />
          </Form.Item>
        )}
      </Form>
    </Modal>
  );
}
