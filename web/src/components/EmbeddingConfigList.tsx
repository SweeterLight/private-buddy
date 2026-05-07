import ConfigList from './ConfigList';
import { embeddingConfigApi } from '../services/api';
import type { EmbeddingConfig } from '../types';

const FORM_FIELDS = [
  { name: 'name', labelKey: 'name', placeholderKey: 'namePlaceholder', required: true },
  { name: 'model_id', labelKey: 'modelId', placeholderKey: 'modelIdPlaceholder', required: true },
  { name: 'base_url', labelKey: 'baseUrl', placeholderKey: 'baseUrlPlaceholder', required: true },
  { name: 'api_key', labelKey: 'apiKey', placeholderKey: 'apiKeyPlaceholder', required: true, type: 'password' as const },
  { name: 'description', labelKey: 'description', placeholderKey: 'descriptionPlaceholder', type: 'textarea' as const, rows: 2 },
];

interface EmbeddingConfigListProps {
  onSelectConfig?: (config: EmbeddingConfig | null) => void;
  showCreate?: boolean;
  onCreateClose?: () => void;
}

export default function EmbeddingConfigList({ onSelectConfig, showCreate, onCreateClose }: EmbeddingConfigListProps) {
  return (
    <ConfigList<EmbeddingConfig>
      iconType="embedding"
      api={embeddingConfigApi}
      formFields={FORM_FIELDS}
      i18nPrefix="embeddingConfig"
      displayField="model_id"
      onSelectConfig={onSelectConfig}
      showCreate={showCreate}
      onCreateClose={onCreateClose}
    />
  );
}
