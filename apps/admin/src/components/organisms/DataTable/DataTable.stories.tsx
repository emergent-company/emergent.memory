import type { Meta, StoryObj } from '@storybook/react';
import React, { useState } from 'react';
import { DataTable } from './index';
import {
  TableDataItem,
  ColumnDef,
  FilterConfig,
  BulkAction,
  RowAction,
} from './types';
import { Icon } from '@/components/atoms/Icon';

interface User extends TableDataItem {
  name: string;
  email: string;
  role: 'admin' | 'user' | 'editor';
  status: 'active' | 'inactive' | 'pending';
  lastLogin: string;
}

const mockUsers: User[] = [
  {
    id: '1',
    name: 'Alice Johnson',
    email: 'alice@example.com',
    role: 'admin',
    status: 'active',
    lastLogin: '2025-10-22T10:30:00Z',
  },
  {
    id: '2',
    name: 'Bob Smith',
    email: 'bob@example.com',
    role: 'user',
    status: 'inactive',
    lastLogin: '2025-10-20T15:45:00Z',
  },
  {
    id: '3',
    name: 'Charlie Brown',
    email: 'charlie@example.com',
    role: 'editor',
    status: 'active',
    lastLogin: '2025-10-21T09:15:00Z',
  },
  {
    id: '4',
    name: 'Diana Prince',
    email: 'diana@example.com',
    role: 'user',
    status: 'pending',
    lastLogin: '2025-10-19T12:00:00Z',
  },
  {
    id: '5',
    name: 'Edward Norton',
    email: 'edward@example.com',
    role: 'user',
    status: 'active',
    lastLogin: '2025-10-22T08:00:00Z',
  },
];

const columns: ColumnDef<User>[] = [
  {
    key: 'name',
    label: 'User',
    sortable: true,
    render: (user) => (
      <div className="flex items-center gap-3">
        <div className="avatar placeholder">
          <div className="bg-neutral text-neutral-content rounded-full w-8">
            <span>{user.name.charAt(0)}</span>
          </div>
        </div>
        <div className="font-medium">{user.name}</div>
      </div>
    ),
  },
  { key: 'email', label: 'Email', sortable: true },
  {
    key: 'role',
    label: 'Role',
    render: (user) => (
      <span className="capitalize badge badge-ghost badge-sm">{user.role}</span>
    ),
  },
  {
    key: 'status',
    label: 'Status',
    render: (user) => {
      const colors = {
        active: 'badge-success',
        inactive: 'badge-error',
        pending: 'badge-warning',
      };
      return (
        <span className={`badge ${colors[user.status]} badge-sm`}>
          {user.status}
        </span>
      );
    },
  },
  {
    key: 'lastLogin',
    label: 'Last Login',
    sortable: true,
    render: (user) => new Date(user.lastLogin).toLocaleDateString(),
  },
];

const meta: Meta<typeof DataTable> = {
  title: 'Organisms/DataTable',
  component: DataTable,
  parameters: {
    layout: 'padded',
  },
};

export default meta;

type Story = StoryObj<typeof DataTable>;

export const Default: Story = {
  args: {
    data: mockUsers,
    columns: columns,
    paginationItemLabel: 'users',
  },
};

export const Loading: Story = {
  args: {
    data: [],
    columns: columns,
    loading: true,
  },
};

export const Empty: Story = {
  args: {
    data: [],
    columns: columns,
    emptyMessage: 'No users found in the system.',
    emptyIcon: 'lucide--users',
  },
};

export const WithSelectionAndFilters: Story = {
  render: () => {
    const [selected, setSelected] = useState<string[]>([]);

    const filters: FilterConfig<User>[] = [
      {
        key: 'status',
        label: 'Status',
        icon: 'lucide--activity',
        options: [
          { value: 'active', label: 'Active' },
          { value: 'inactive', label: 'Inactive' },
          { value: 'pending', label: 'Pending' },
        ],
        getValue: (item) => item.status,
      },
      {
        key: 'role',
        label: 'Role',
        icon: 'lucide--shield',
        options: [
          { value: 'admin', label: 'Admin' },
          { value: 'user', label: 'User' },
          { value: 'editor', label: 'Editor' },
        ],
        getValue: (item) => item.role,
      },
    ];

    const bulkActions: BulkAction<User>[] = [
      {
        key: 'delete',
        label: 'Delete',
        icon: 'lucide--trash-2',
        variant: 'error',
        onAction: (ids) =>
          alert(`Deleting ${ids.length} users: ${ids.join(', ')}`),
      },
      {
        key: 'export',
        label: 'Export CSV',
        icon: 'lucide--download',
        variant: 'ghost',
        onAction: (ids) => alert(`Exporting ${ids.length} users`),
      },
    ];

    const rowActions: RowAction<User>[] = [
      {
        label: 'Edit',
        icon: 'lucide--edit',
        onAction: (user) => alert(`Editing ${user.name}`),
      },
      {
        label: 'Deactivate',
        icon: 'lucide--user-x',
        variant: 'error',
        onAction: (user) => alert(`Deactivating ${user.name}`),
        hidden: (user) => user.status === 'inactive',
      },
    ];

    return (
      <DataTable
        data={mockUsers}
        columns={columns}
        enableSelection
        enableSearch
        filters={filters}
        bulkActions={bulkActions}
        rowActions={rowActions}
        onSelectionChange={setSelected}
        getSearchText={(u) => `${u.name} ${u.email}`}
        totalCount={150} // Enable "Select all matching items in database"
      />
    );
  },
};

export const CardView: Story = {
  args: {
    data: mockUsers,
    columns: columns,
    enableViewToggle: true,
    defaultView: 'cards',
    renderCard: (user, isSelected, onSelect) => (
      <div
        className={`card bg-base-100 shadow-sm border ${
          isSelected ? 'border-primary' : 'border-base-300'
        }`}
      >
        <div className="card-body p-4">
          <div className="flex justify-between items-start">
            <div className="flex items-center gap-3">
              <input
                type="checkbox"
                className="checkbox checkbox-sm"
                checked={isSelected}
                onChange={(e) => onSelect(e.target.checked)}
              />
              <div className="font-bold">{user.name}</div>
            </div>
            <span className="badge badge-sm">{user.role}</span>
          </div>
          <p className="text-sm opacity-70">{user.email}</p>
          <div className="card-actions justify-end mt-4">
            <button className="btn btn-xs btn-ghost">View Profile</button>
          </div>
        </div>
      </div>
    ),
  },
};

export const Pagination: Story = {
  args: {
    data: mockUsers,
    columns: columns,
    pagination: {
      page: 1,
      totalPages: 10,
      total: 50,
      limit: 5,
      hasPrev: false,
      hasNext: true,
    },
    onPageChange: (page) => console.log('Page changed to', page),
  },
};
