import { Modal, Spin } from 'antd';
import { formatMessageTime } from '../utils/time';
import type { Interaction } from '../types';
import { INTERACTION_TYPE_REQUEST } from '../types';

interface InteractionModalProps {
  visible: boolean;
  loading: boolean;
  interactions: Interaction[];
  onClose: () => void;
}

export default function InteractionModal({ visible, loading, interactions, onClose }: InteractionModalProps) {
  const iterationMap = new Map<number, Interaction[]>();
  interactions.forEach(interaction => {
    const list = iterationMap.get(interaction.iteration) || [];
    list.push(interaction);
    iterationMap.set(interaction.iteration, list);
  });

  return (
    <Modal
      title="Interaction Records"
      open={visible}
      onCancel={onClose}
      footer={null}
      width={800}
    >
      {loading ? (
        <div style={{ textAlign: 'center', padding: '20px' }}>
          <Spin />
        </div>
      ) : (
        <div style={{ maxHeight: '600px', overflowY: 'auto' }}>
          {interactions.length === 0 ? (
            <div style={{ textAlign: 'center', color: 'var(--color-text-placeholder)', padding: '20px' }}>
              No interaction records found
            </div>
          ) : (
            Array.from(iterationMap.entries()).map(([iteration, items]) => (
              <div key={`iter-${iteration}`} style={{ marginBottom: '16px' }}>
                <div style={{ fontWeight: 600, fontSize: '14px', marginBottom: '8px', color: '#374151' }}>
                  Iteration {iteration}
                </div>
                {items.map(interaction => {
                  const typeLabel = interaction.type === INTERACTION_TYPE_REQUEST ? 'Request' : 'Response';
                  const typeColor = interaction.type === INTERACTION_TYPE_REQUEST ? '#3b82f6' : '#10b981';
                  return (
                    <div key={`interaction-${interaction.id}`} style={{ marginLeft: '16px', marginBottom: '8px' }}>
                      <div style={{ fontSize: '12px', marginBottom: '4px' }}>
                        <span style={{ color: typeColor, fontWeight: 500 }}>{typeLabel}</span>
                        <span style={{ color: 'var(--color-text-placeholder)', marginLeft: '8px' }}>
                          {formatMessageTime(new Date(interaction.updated_at))}
                        </span>
                      </div>
                      <pre style={{
                        whiteSpace: 'pre-wrap',
                        wordBreak: 'break-word',
                        fontSize: '12px',
                        lineHeight: '1.5',
                        margin: 0,
                        padding: '8px',
                        background: '#f9fafb',
                        borderRadius: '4px',
                        border: '1px solid #e5e7eb',
                        maxHeight: '300px',
                        overflowY: 'auto',
                      }}>
                        {(() => {
                          try {
                            return JSON.stringify(JSON.parse(interaction.data), null, 2);
                          } catch {
                            return interaction.data;
                          }
                        })()}
                      </pre>
                    </div>
                  );
                })}
              </div>
            ))
          )}
        </div>
      )}
    </Modal>
  );
}
