import React from 'react';
import { UserOutlined, RobotOutlined, ApiOutlined, SearchOutlined, GlobalOutlined } from '@ant-design/icons';
import { getAvatarUrl } from '../services/api';

export type IconType = 'agent' | 'llm' | 'embedding' | 'search' | 'language';

interface ConfigIconProps {
  type: IconType;
  size?: number;
  iconSize?: number;
  borderRadius?: string;
  marginBottom?: number;
}

const ICON_MAP: Record<IconType, { icon: React.ReactNode; colorVar: string; bgVar: string }> = {
  agent: { icon: <UserOutlined />, colorVar: 'var(--color-primary)', bgVar: 'var(--color-primary-bg)' },
  llm: { icon: <RobotOutlined />, colorVar: 'var(--color-llm)', bgVar: 'var(--color-llm-bg)' },
  embedding: { icon: <ApiOutlined />, colorVar: 'var(--color-embedding)', bgVar: 'var(--color-embedding-bg)' },
  search: { icon: <SearchOutlined />, colorVar: 'var(--color-search)', bgVar: 'var(--color-search-bg)' },
  language: { icon: <GlobalOutlined />, colorVar: 'var(--color-language)', bgVar: 'var(--color-language-bg)' },
};

const ConfigIcon: React.FC<ConfigIconProps> = ({
  type,
  size = 36,
  iconSize = 16,
  borderRadius = '8px',
  marginBottom,
}) => {
  const config = ICON_MAP[type];

  return (
    <div
      style={{
        width: size,
        height: size,
        borderRadius,
        backgroundColor: config.bgVar,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        color: config.colorVar,
        fontSize: iconSize,
        flexShrink: 0,
        marginBottom,
      }}
    >
      {config.icon}
    </div>
  );
};

interface AgentAvatarProps {
  avatar: string;
  size?: number;
  iconSize?: number;
  borderRadius?: string;
}

const AgentAvatar: React.FC<AgentAvatarProps> = ({
  avatar,
  size = 44,
  iconSize = 20,
  borderRadius = '10px',
}) => {
  const avatarUrl = getAvatarUrl(avatar);

  if (avatarUrl) {
    return (
      <img
        src={avatarUrl}
        alt="avatar"
        style={{
          width: size,
          height: size,
          borderRadius,
          objectFit: 'cover',
        }}
      />
    );
  }

  return <ConfigIcon type="agent" size={size} iconSize={iconSize} borderRadius={borderRadius} />;
};

export { ConfigIcon };
export default AgentAvatar;
