import type { Meta, StoryObj } from '@storybook/react';
import React from 'react';
import { DataTable } from '@/components/organisms/DataTable';
import { ColumnDef, RowAction } from '@/components/organisms/DataTable/types';
import { Icon } from '@/components/atoms/Icon';
import { TableAvatarCell } from '@/components/molecules/TableAvatarCell';

const meta: Meta<typeof DataTable> = {
  title: 'Examples/Table Gallery',
  component: DataTable,
  parameters: {
    layout: 'padded',
  },
};

export default meta;

/**
 * UTILITY MOLECULES (Internal for stories)
 */

const StatusBadge = ({ status }: { status: string }) => {
  const getStatusColor = (s: string) => {
    switch (s.toLowerCase()) {
      case 'active':
      case 'success':
      case 'completed':
      case 'online':
        return 'badge-success';
      case 'pending':
      case 'processing':
      case 'away':
        return 'badge-warning';
      case 'failed':
      case 'error':
      case 'inactive':
      case 'offline':
        return 'badge-error';
      default:
        return 'badge-ghost';
    }
  };

  return (
    <span
      className={`badge ${getStatusColor(
        status
      )} badge-sm font-medium capitalize`}
    >
      {status}
    </span>
  );
};

const DateCell = ({ date }: { date: string }) => {
  const d = new Date(date);
  return (
    <div className="flex flex-col">
      <span className="text-sm font-medium">{d.toLocaleDateString()}</span>
      <span className="text-xs opacity-50">
        {d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}
      </span>
    </div>
  );
};

/**
 * EXAMPLE: MEMBER MANAGEMENT
 */
export const MemberManagement: StoryObj = {
  render: () => {
    const data = [
      {
        id: '1',
        name: 'Maciej Kucharz',
        email: 'maciej@emergent.ai',
        role: 'Owner',
        status: 'online',
        joined: '2024-01-15',
      },
      {
        id: '2',
        name: 'Alice Smith',
        email: 'alice@emergent.ai',
        role: 'Admin',
        status: 'away',
        joined: '2024-03-20',
      },
      {
        id: '3',
        name: 'Bob Wilson',
        email: 'bob@emergent.ai',
        role: 'Member',
        status: 'offline',
        joined: '2024-06-10',
      },
    ];

    const columns: ColumnDef<any>[] = [
      {
        key: 'name',
        label: 'Member',
        render: (row) => (
          <TableAvatarCell name={row.name} subtitle={row.email} />
        ),
      },
      {
        key: 'role',
        label: 'Role',
        render: (row) => <span className="text-sm opacity-80">{row.role}</span>,
      },
      {
        key: 'status',
        label: 'Status',
        render: (row) => <StatusBadge status={row.status} />,
      },
      {
        key: 'joined',
        label: 'Joined',
        render: (row) => (
          <span className="text-sm opacity-60">
            {new Date(row.joined).toLocaleDateString()}
          </span>
        ),
      },
    ];

    const rowActions: RowAction<any>[] = [
      { label: 'Edit Role', icon: 'lucide--shield', onAction: () => {} },
      {
        label: 'Remove',
        icon: 'lucide--user-minus',
        variant: 'error',
        onAction: () => {},
      },
    ];

    return (
      <div className="space-y-4">
        <div>
          <h2 className="text-2xl font-bold">Team Members</h2>
          <p className="text-base-content/60">
            Manage your team and their access permissions.
          </p>
        </div>
        <DataTable
          data={data}
          columns={columns}
          rowActions={rowActions}
          useDropdownActions
          enableSearch
          toolbarActions={
            <button className="btn btn-primary btn-sm">Invite Member</button>
          }
        />
      </div>
    );
  },
};

/**
 * EXAMPLE: DOCUMENT BROWSER
 */
export const DocumentBrowser: StoryObj = {
  render: () => {
    const data = [
      {
        id: '1',
        name: 'Q4_Report.pdf',
        size: '2.4 MB',
        type: 'PDF',
        updated: '2025-10-22T14:00:00Z',
        status: 'completed',
      },
      {
        id: '2',
        name: 'Project_Spec.docx',
        size: '850 KB',
        type: 'DOCX',
        updated: '2025-10-21T09:30:00Z',
        status: 'processing',
      },
      {
        id: '3',
        name: 'Logo_Final.png',
        size: '1.2 MB',
        type: 'PNG',
        updated: '2025-10-18T16:45:00Z',
        status: 'completed',
      },
      {
        id: '4',
        name: 'Data_Export.csv',
        size: '15.7 MB',
        type: 'CSV',
        updated: '2025-10-15T11:20:00Z',
        status: 'failed',
      },
    ];

    const getFileIcon = (type: string) => {
      switch (type) {
        case 'PDF':
          return 'lucide--file-text';
        case 'CSV':
          return 'lucide--table';
        case 'PNG':
          return 'lucide--image';
        default:
          return 'lucide--file';
      }
    };

    const columns: ColumnDef<any>[] = [
      {
        key: 'name',
        label: 'Filename',
        render: (row) => (
          <div className="flex items-center gap-3">
            <div className="p-2 bg-base-200 rounded">
              <Icon
                icon={getFileIcon(row.type)}
                className="size-5 text-primary"
              />
            </div>
            <div className="flex flex-col">
              <span className="font-medium text-sm">{row.name}</span>
              <span className="text-xs opacity-50">
                {row.type} • {row.size}
              </span>
            </div>
          </div>
        ),
      },
      {
        key: 'status',
        label: 'Processing',
        render: (row) => <StatusBadge status={row.status} />,
      },
      {
        key: 'updated',
        label: 'Last Modified',
        render: (row) => <DateCell date={row.updated} />,
      },
    ];

    return (
      <div className="space-y-4">
        <div className="flex justify-between items-end">
          <div>
            <h2 className="text-2xl font-bold">Documents</h2>
            <p className="text-base-content/60">
              Knowledge base source files and extractions.
            </p>
          </div>
          <div className="flex gap-2">
            <button className="btn btn-outline btn-sm">Download All</button>
            <button className="btn btn-primary btn-sm">Upload File</button>
          </div>
        </div>
        <DataTable
          data={data}
          columns={columns}
          enableSelection
          bulkActions={[
            {
              key: 'delete',
              label: 'Delete',
              icon: 'lucide--trash',
              variant: 'error',
              onAction: () => {},
            },
            {
              key: 'move',
              label: 'Move to Folder',
              icon: 'lucide--folder-input',
              variant: 'ghost',
              onAction: () => {},
            },
          ]}
        />
      </div>
    );
  },
};

/**
 * EXAMPLE: CHAT SESSIONS (Migration Candidate)
 */
export const ChatSessions: StoryObj = {
  render: () => {
    const data = [
      {
        id: 'sess_1',
        user: 'Guest_283',
        messages: 12,
        tokens: 4500,
        lastActivity: '2025-10-22T16:00:00Z',
        model: 'GPT-4o',
      },
      {
        id: 'sess_2',
        user: 'Maciej K.',
        messages: 45,
        tokens: 12800,
        lastActivity: '2025-10-22T15:30:00Z',
        model: 'Claude 3.5 Sonnet',
      },
      {
        id: 'sess_3',
        user: 'Alice S.',
        messages: 8,
        tokens: 1200,
        lastActivity: '2025-10-21T10:00:00Z',
        model: 'GPT-4o-mini',
      },
    ];

    const columns: ColumnDef<any>[] = [
      {
        key: 'user',
        label: 'User / Session ID',
        render: (row) => (
          <div className="flex flex-col">
            <span className="font-medium">{row.user}</span>
            <span className="text-[10px] font-mono opacity-40 uppercase tracking-widest">
              {row.id}
            </span>
          </div>
        ),
      },
      {
        key: 'model',
        label: 'Model',
        render: (row) => (
          <div className="flex items-center gap-2">
            <Icon icon="lucide--bot" className="size-4 opacity-40" />
            <span className="text-sm">{row.model}</span>
          </div>
        ),
      },
      {
        key: 'usage',
        label: 'Usage',
        render: (row) => (
          <div className="text-xs">
            <div className="font-medium">{row.messages} messages</div>
            <div className="opacity-50">
              {row.tokens.toLocaleString()} tokens
            </div>
          </div>
        ),
      },
      {
        key: 'lastActivity',
        label: 'Last Active',
        render: (row) => <DateCell date={row.lastActivity} />,
      },
    ];

    return (
      <div className="space-y-4">
        <div className="flex items-center gap-3">
          <div className="p-3 bg-secondary/10 rounded-xl">
            <Icon
              icon="lucide--message-square"
              className="size-6 text-secondary"
            />
          </div>
          <div>
            <h2 className="text-2xl font-bold">Chat Sessions</h2>
            <p className="text-base-content/60 text-sm italic">
              Showing how the raw HTML table can be standardized into a rich
              list.
            </p>
          </div>
        </div>
        <DataTable
          data={data}
          columns={columns}
          rowActions={[
            {
              label: 'View Transcripts',
              icon: 'lucide--external-link',
              onAction: () => {},
            },
            {
              label: 'Delete',
              icon: 'lucide--trash',
              variant: 'error',
              onAction: () => {},
            },
          ]}
        />
      </div>
    );
  },
};
