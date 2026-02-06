import type { Meta, StoryObj } from '@storybook/react';
import { Tooltip, type TooltipProps } from './index';

const meta: Meta<typeof Tooltip> = {
  title: 'Atoms/Tooltip',
  component: Tooltip,
  args: {
    placement: 'top',
    content: <span className="font-medium">Tooltip content</span>,
  },
};
export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  render: (args: TooltipProps) => (
    <div className="flex justify-center p-10">
      <Tooltip {...args}>
        <button className="btn btn-outline">Hover me</button>
      </Tooltip>
    </div>
  ),
};

export const Placements: Story = {
  render: () => (
    <div className="grid grid-cols-2 gap-8 p-10">
      {(['top', 'right', 'bottom', 'left'] as const).map((p) => (
        <Tooltip key={p} placement={p} content={<span>{p} tooltip</span>}>
          <button className="btn btn-sm">{p}</button>
        </Tooltip>
      ))}
    </div>
  ),
};

export const Alignments: Story = {
  render: () => (
    <div className="flex flex-col gap-8 p-10">
      <div className="flex justify-center gap-4">
        <h3 className="w-full text-center font-bold">Top Placement</h3>
      </div>
      <div className="flex justify-center gap-8">
        {(['start', 'center', 'end'] as const).map((a) => (
          <Tooltip
            key={a}
            placement="top"
            align={a}
            content={<span>Aligned {a}</span>}
          >
            <button className="btn btn-sm">{a}</button>
          </Tooltip>
        ))}
      </div>
      <div className="flex justify-center gap-4">
        <h3 className="w-full text-center font-bold">Bottom Placement</h3>
      </div>
      <div className="flex justify-center gap-8">
        {(['start', 'center', 'end'] as const).map((a) => (
          <Tooltip
            key={a}
            placement="bottom"
            align={a}
            content={<span>Aligned {a}</span>}
          >
            <button className="btn btn-sm">{a}</button>
          </Tooltip>
        ))}
      </div>
    </div>
  ),
};

export const Colors: Story = {
  render: () => (
    <div className="grid grid-cols-4 gap-4 p-10">
      {(
        [
          'neutral',
          'primary',
          'secondary',
          'accent',
          'info',
          'success',
          'warning',
          'error',
        ] as const
      ).map((c) => (
        <Tooltip key={c} color={c} content={<span>{c} color</span>}>
          <button className="btn btn-sm">{c}</button>
        </Tooltip>
      ))}
    </div>
  ),
};

/**
 * This story demonstrates that tooltips using the Popover API
 * are NOT clipped by overflow:hidden containers.
 * The tooltip renders in the browser's top layer, bypassing all CSS stacking.
 */
export const InOverflowContainer: Story = {
  render: () => (
    <div className="p-10">
      <h3 className="mb-4 font-bold">
        Tooltip in overflow:hidden container (should NOT be clipped)
      </h3>
      <div
        className="rounded-lg border-2 border-dashed border-warning bg-base-200 p-4"
        style={{ overflow: 'hidden', height: '100px' }}
      >
        <p className="mb-2 text-sm text-base-content/70">
          This container has overflow:hidden
        </p>
        <Tooltip
          placement="top"
          content={
            <span>
              This tooltip should appear ABOVE the container, not clipped!
            </span>
          }
        >
          <button className="btn btn-warning btn-sm">
            Hover me (tooltip goes up)
          </button>
        </Tooltip>
      </div>

      <h3 className="mb-4 mt-8 font-bold">
        Tooltip at bottom of scrollable container
      </h3>
      <div
        className="rounded-lg border-2 border-dashed border-info bg-base-200"
        style={{ overflow: 'auto', height: '150px' }}
      >
        <div className="flex h-[200px] flex-col justify-end p-4">
          <p className="mb-2 text-sm text-base-content/70">
            Scroll down to see the button
          </p>
          <Tooltip
            placement="bottom"
            content={<span>Tooltip below, outside the scroll container!</span>}
          >
            <button className="btn btn-info btn-sm">
              Hover me (tooltip goes down)
            </button>
          </Tooltip>
        </div>
      </div>
    </div>
  ),
};

/**
 * Simulates the DataTable last-row scenario where tooltips were being clipped
 */
export const InTableLastRow: Story = {
  render: () => (
    <div className="p-10">
      <h3 className="mb-4 font-bold">
        Table with overflow:hidden (like DataTable)
      </h3>
      <div className="overflow-hidden rounded-lg border border-base-300">
        <table className="table">
          <thead>
            <tr>
              <th>Name</th>
              <th>Status</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            <tr>
              <td>Item 1</td>
              <td>Active</td>
              <td>
                <Tooltip content="Edit this item">
                  <button className="btn btn-ghost btn-xs">Edit</button>
                </Tooltip>
              </td>
            </tr>
            <tr>
              <td>Item 2</td>
              <td>Pending</td>
              <td>
                <Tooltip content="Edit this item">
                  <button className="btn btn-ghost btn-xs">Edit</button>
                </Tooltip>
              </td>
            </tr>
            <tr>
              <td>Last Item</td>
              <td>Complete</td>
              <td>
                <Tooltip
                  placement="top"
                  content="This tooltip should NOT be clipped by the table container!"
                >
                  <button className="btn btn-ghost btn-xs">Hover me!</button>
                </Tooltip>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  ),
};

export const WithLongContent: Story = {
  render: () => (
    <div className="flex justify-center p-10">
      <Tooltip
        content={
          <div>
            <p className="font-bold">Detailed Information</p>
            <p className="mt-1">
              This is a longer tooltip with multiple lines of text to
              demonstrate how the tooltip handles longer content gracefully.
            </p>
          </div>
        }
      >
        <button className="btn btn-outline">Hover for details</button>
      </Tooltip>
    </div>
  ),
};
