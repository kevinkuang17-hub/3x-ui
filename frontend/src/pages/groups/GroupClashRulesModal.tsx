import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Alert, Form, Modal, Radio } from 'antd';
import { useQuery } from '@tanstack/react-query';

import RoutingChoiceEditor from '@/pages/rule-sets/RoutingChoiceEditor';
import TemplatePicker, { useTemplateGroups } from '@/pages/rule-sets/TemplatePicker';
import { EMPTY_CHOICE, type RoutingChoice, fetchRuleSets } from '@/pages/rule-sets/ruleSetApi';
import { keys } from '@/api/queryKeys';

interface Props {
  open: boolean;
  groupName: string | null;
  initialRules: string;
  initialChoice: RoutingChoice;
  saving: boolean;
  onClose: () => void;
  onSubmit: (rules: string, choice: RoutingChoice) => Promise<boolean>;
}

export default function GroupClashRulesModal({
  open,
  groupName,
  initialRules,
  initialChoice,
  saving,
  onClose,
  onSubmit,
}: Props) {
  const { t } = useTranslation();
  const followsGlobal =
    !initialRules.trim() && initialChoice.ruleSets.length === 0 && !initialChoice.extraRules.trim();
  const [own, setOwn] = useState(!followsGlobal);
  const [template, setTemplate] = useState(initialRules);
  const [choice, setChoice] = useState<RoutingChoice>(initialChoice);
  const setsQuery = useQuery({
    queryKey: keys.ruleSets.list(),
    queryFn: fetchRuleSets,
    enabled: open && own,
  });
  const groupsQuery = useTemplateGroups(own ? template : '');

  async function confirm() {
    if (!own) {
      if (await onSubmit('', EMPTY_CHOICE)) onClose();
      return;
    }
    if (await onSubmit(template.trim(), choice)) onClose();
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
      width={own ? 800 : 520}
      destroyOnHidden
    >
      <Alert
        type="info"
        showIcon
        title={t('pages.groups.clashRulesDesc')}
        style={{ marginBottom: 16 }}
      />
      <Form layout="vertical">
        <Form.Item>
          <Radio.Group
            value={own ? 'own' : 'global'}
            onChange={(e) => setOwn(e.target.value === 'own')}
            options={[
              { value: 'global', label: t('pages.groups.clashRulesFollowGlobal') },
              { value: 'own', label: t('pages.groups.clashRulesOwn') },
            ]}
          />
        </Form.Item>
      </Form>
      {own && (
        <>
          <Alert
            type="warning"
            showIcon
            style={{ marginBottom: 12 }}
            title={t('pages.groups.clashRulesOwnDesc')}
          />
          <TemplatePicker
            value={template}
            onChange={setTemplate}
            groups={groupsQuery.data ?? []}
            groupsError={
              groupsQuery.error
                ? t('pages.ruleSets.templateGroupsFailed', { msg: groupsQuery.error.message })
                : undefined
            }
          />
          <RoutingChoiceEditor
            sets={setsQuery.data ?? []}
            loading={setsQuery.isFetching}
            value={choice}
            onChange={setChoice}
            groups={groupsQuery.data ?? []}
          />
        </>
      )}
    </Modal>
  );
}
