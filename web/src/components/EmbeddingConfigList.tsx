import React, { useState, useEffect } from 'react';
import { Form, Input, Button, message, Spin } from 'antd';
import { SaveOutlined, ReloadOutlined } from '@ant-design/icons';
import { useTranslation } from 'react-i18next';
import type { EmbeddingConfig } from '../types';
import { embeddingConfigApi } from '../services/api';

const EmbeddingConfigForm: React.FC<{ onCreated?: () => void }> = ({ onCreated }) => {
  const { t } = useTranslation();
  const [form] = Form.useForm();
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [config, setConfig] = useState<EmbeddingConfig | null>(null);

  useEffect(() => {
    loadConfig();
  }, []);

  const loadConfig = async () => {
    setLoading(true);
    try {
      const response = await embeddingConfigApi.get();
      setConfig(response.data);
      form.setFieldsValue(response.data);
    } catch (error) {
      message.error(t('embeddingConfig.loadError'));
    } finally {
      setLoading(false);
    }
  };

  const handleSave = async (values: Partial<EmbeddingConfig>) => {
    setSaving(true);
    try {
      const response = await embeddingConfigApi.update(values);
      setConfig(response.data);
      message.success(t('embeddingConfig.saveSuccess'));
      onCreated?.();
    } catch (error) {
      message.error(t('embeddingConfig.saveError'));
    } finally {
      setSaving(false);
    }
  };

  const handleReset = () => {
    if (config) {
      form.setFieldsValue(config);
    }
  };

  if (loading) {
    return (
      <div style={{ display: 'flex', justifyContent: 'center', padding: '40px' }}>
        <Spin />
      </div>
    );
  }

  return (
    <div className="config-form-container">
      <Form
        form={form}
        layout="vertical"
        onFinish={handleSave}
        initialValues={config || undefined}
      >
        <Form.Item
          name="name"
          label={t('embeddingConfig.name')}
          rules={[{ required: true, message: t('embeddingConfig.namePlaceholder') }]}
        >
          <Input placeholder={t('embeddingConfig.namePlaceholder')} />
        </Form.Item>

        <Form.Item
          name="model_id"
          label={t('embeddingConfig.modelId')}
          rules={[{ required: true, message: t('embeddingConfig.modelIdPlaceholder') }]}
        >
          <Input placeholder={t('embeddingConfig.modelIdPlaceholder')} />
        </Form.Item>

        <Form.Item
          name="base_url"
          label={t('embeddingConfig.baseUrl')}
          rules={[{ required: true, message: t('embeddingConfig.baseUrlPlaceholder') }]}
        >
          <Input placeholder={t('embeddingConfig.baseUrlPlaceholder')} />
        </Form.Item>

        <Form.Item
          name="api_key"
          label={t('embeddingConfig.apiKey')}
          rules={[{ required: true, message: t('embeddingConfig.apiKeyPlaceholder') }]}
        >
          <Input.Password placeholder={t('embeddingConfig.apiKeyPlaceholder')} />
        </Form.Item>

        <Form.Item
          name="description"
          label={t('embeddingConfig.description')}
        >
          <Input.TextArea rows={3} placeholder={t('embeddingConfig.descriptionPlaceholder')} />
        </Form.Item>

        <Form.Item>
          <Button
            type="primary"
            htmlType="submit"
            icon={<SaveOutlined />}
            loading={saving}
            style={{ marginRight: 8 }}
          >
            {t('common.save')}
          </Button>
          <Button
            icon={<ReloadOutlined />}
            onClick={handleReset}
          >
            {t('common.reset')}
          </Button>
        </Form.Item>
      </Form>
    </div>
  );
};

export default EmbeddingConfigForm;
