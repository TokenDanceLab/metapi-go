import React from 'react';
import MobileBatchBar from './MobileBatchBar.js';

type ResponsiveBatchActionBarProps = {
  isMobile: boolean;
  info: React.ReactNode;
  children: React.ReactNode;
  desktopStyle?: React.CSSProperties;
  infoStyle?: React.CSSProperties;
};

export default function ResponsiveBatchActionBar({
  isMobile,
  info,
  children,
  desktopStyle,
  infoStyle,
}: ResponsiveBatchActionBarProps) {
  if (isMobile) {
    return <MobileBatchBar info={info}>{children}</MobileBatchBar>;
  }

  return (
    <div className="card batch-action-bar" style={desktopStyle}>
      <span className="batch-action-bar-info" style={infoStyle}>{info}</span>
      {children}
    </div>
  );
}
