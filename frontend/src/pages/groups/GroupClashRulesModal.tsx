import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Alert, Divider, Form, Modal, Radio } from 'antd';
import { useQuery } from '@tanstack/react-query';

import RoutingChoiceEditor from '@/pages/rule-sets/RoutingChoiceEditor';
import TemplatePicker, { useTemplateGroups } from '@/pages/rule-sets/TemplatePicker';
import DnsEditor, { type DnsConfig, EMPTY_DNS } from '@/pages/rule-sets/DnsEditor';
import { EMPTY_CHOICE, type RoutingChoice, fetchRuleSets } from '@/pages/rule-sets/ruleSetApi';
import { keys } from '@/api/queryKeys';

interface Props {
  open: boolean;
  groupName: string | null;
  initialRules: string;
  initialChoice: RoutingChoice;
  // The group's own DNS; null follows the global DNS.
  initialDns: DnsConfig | null;
  saving: boolean;
  onClose: () => void;
  onSubmit: (rules: string, choice: RoutingChoice, dns: DnsConfig | null) => Promise<boolean>;
}

export default function GroupClashRulesModal({
  open,
  groupName,
  initialRules,
  initialChoice,
  initialDns,
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
  const [ownDns, setOwnDns] = useState(initialDns !== null);
  const [dns, setDns] = useState<DnsConfig>(() =>
    initialDns
      ? {
          ...EMPTY_DNS,
          ...initialDns,
          domestic: initialDns.domestic ?? [],
          foreign: initialDns.foreign ?? [],
        }
      : EMPTY_DNS,
  );
  const setsQuery = useQuery({
    queryKey: keys.ruleSets.list(),
    queryFn: fetchRuleSets,
    enabled: open && own,
  });
  const groupsQuery = useTemplateGroups(own ? template : '');

  async function confirm() {
    const groupDns = ownDns ? dns : null;
    if (!own) {
      if (await onSubmit('', EMPTY_CHOICE, groupDns)) onClose();
      return;
    }
    if (await onSubmit(template.trim(), choice, groupDns)) onClose();
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
      width={800}
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
      <Divider titlePlacement="start">{t('pages.ruleSets.dnsTitle')}</Divider>
      <Radio.Group
        style={{ marginBottom: 12 }}
        value={ownDns ? 'own' : 'global'}
        onChange={(e) => setOwnDns(e.target.value === 'own')}
        options={[
          { value: 'global', label: t('pages.groups.dnsFollowGlobal') },
          { value: 'own', label: t('pages.groups.dnsOwn') },
        ]}
      />
      {ownDns && <DnsEditor value={dns} onChange={setDns} />}
    </Modal>
  );
}
