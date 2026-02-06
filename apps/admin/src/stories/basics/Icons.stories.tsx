import type { Meta, StoryObj } from '@storybook/react';
import React, { useState } from 'react';
import { Icon } from '@/components/atoms/Icon';

/**
 * Safelist for Tailwind JIT - these class names must be statically scannable.
 * The icon classes below are dynamically constructed in the component,
 * so we list them here to ensure CSS is generated.
 *
 * lucide--home lucide--user lucide--settings lucide--search lucide--mail
 * lucide--phone lucide--lock lucide--eye lucide--eye-off lucide--plus
 * lucide--minus lucide--x lucide--check lucide--chevron-down lucide--chevron-up
 * lucide--chevron-left lucide--chevron-right lucide--trash-2 lucide--edit
 * lucide--save lucide--download lucide--upload lucide--external-link
 * lucide--info lucide--alert-circle lucide--activity lucide--shield
 * lucide--map-pin lucide--clock lucide--message-square lucide--file-text
 * lucide--table lucide--image lucide--bot
 * hugeicons--ai-innovation-01 hugeicons--access hugeicons--analysis-text-link
 * ri--github-fill ri--google-fill ri--slack-line
 */

const meta: Meta = {
  title: 'Basics/Icons',
  parameters: {
    layout: 'padded',
  },
};

export default meta;

const ICON_SETS = {
  lucide: [
    'home',
    'user',
    'settings',
    'search',
    'mail',
    'phone',
    'lock',
    'eye',
    'eye-off',
    'plus',
    'minus',
    'x',
    'check',
    'chevron-down',
    'chevron-up',
    'chevron-left',
    'chevron-right',
    'trash-2',
    'edit',
    'save',
    'download',
    'upload',
    'external-link',
    'info',
    'alert-circle',
    'activity',
    'shield',
    'map-pin',
    'clock',
    'message-square',
    'file-text',
    'table',
    'image',
    'bot',
  ],
  hugeicons: ['ai-innovation-01', 'access', 'analysis-text-link'],
  ri: ['github-fill', 'google-fill', 'slack-line'],
};

const GalleryComponent = () => {
  const [search, setSearch] = useState('');

  return (
    <div className="space-y-12">
      <section>
        <div className="flex flex-col sm:flex-row justify-between items-start sm:items-center gap-4 mb-8">
          <div>
            <h2 className="text-2xl font-bold">Icon Gallery</h2>
            <p className="text-base-content/60">
              Powered by @iconify/tailwind4 plugin.
            </p>
          </div>
          <label className="input input-bordered input-sm min-w-64">
            <Icon icon="lucide--search" className="opacity-50 size-4" />
            <input
              type="search"
              placeholder="Search common icons..."
              className="grow"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
            />
          </label>
        </div>

        <div className="space-y-12">
          {Object.entries(ICON_SETS).map(([prefix, icons]) => {
            const filtered = icons.filter((i) =>
              i.includes(search.toLowerCase())
            );
            if (filtered.length === 0) return null;

            return (
              <div key={prefix} className="space-y-4">
                <h3 className="text-sm font-bold uppercase tracking-widest opacity-40 border-b pb-1">
                  {prefix}
                </h3>
                <div className="grid grid-cols-2 sm:grid-cols-4 md:grid-cols-6 lg:grid-cols-8 gap-4">
                  {filtered.map((name) => {
                    const iconClass = `${prefix}--${name}`;
                    return (
                      <div
                        key={name}
                        className="flex flex-col items-center justify-center p-4 bg-base-200 rounded-xl hover:bg-primary/10 transition-colors group cursor-pointer"
                        onClick={() => {
                          navigator.clipboard.writeText(iconClass);
                          alert(`Copied: ${iconClass}`);
                        }}
                      >
                        <Icon
                          icon={iconClass}
                          className="size-8 mb-2 group-hover:scale-110 transition-transform text-base-content"
                        />
                        <span className="text-[10px] font-mono opacity-50 text-center break-all">
                          {name}
                        </span>
                      </div>
                    );
                  })}
                </div>
              </div>
            );
          })}
        </div>
      </section>

      <section className="bg-base-200 p-8 rounded-2xl border border-base-content/5">
        <h2 className="text-xl font-bold mb-4">Usage Guide</h2>
        <div className="space-y-4">
          <p>
            Use the <code>Icon</code> atom component with the following pattern:
          </p>
          <pre className="bg-base-300 p-4 rounded-lg overflow-x-auto text-sm">
            <code>{`<Icon icon="lucide--home" className="size-5 text-primary" />`}</code>
          </pre>
          <div className="flex items-center gap-2 text-sm">
            <Icon icon="lucide--info" className="text-info size-4" />
            <span>
              Available prefixes: <code>lucide</code>, <code>hugeicons</code>,{' '}
              <code>ri</code>.
            </span>
          </div>
        </div>
      </section>
    </div>
  );
};

export const Gallery: StoryObj = {
  render: () => <GalleryComponent />,
};
