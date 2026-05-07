import { type ReactNode } from 'react';
import { Button } from 'antd';
import { ArrowLeftOutlined, PlusOutlined } from '@ant-design/icons';

interface PanelDetailProps {
  title: string;
  onBack: () => void;
  onAdd?: () => void;
  children: ReactNode;
}

export default function PanelDetail({ title, onBack, onAdd, children }: PanelDetailProps) {
  return (
    <div className="panel-detail">
      <div className="panel-detail-header">
        <Button
          type="text"
          icon={<ArrowLeftOutlined />}
          onClick={onBack}
          style={{ color: 'var(--color-text-secondary)' }}
        />
        <span className="panel-detail-title">{title}</span>
        {onAdd && (
          <Button
            type="text"
            icon={<PlusOutlined />}
            onClick={onAdd}
            style={{ color: 'var(--color-text-secondary)' }}
          />
        )}
      </div>
      <div className="panel-detail-body">
        {children}
      </div>
    </div>
  );
}
