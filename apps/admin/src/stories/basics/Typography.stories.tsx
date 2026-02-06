import type { Meta, StoryObj } from '@storybook/react';
import React from 'react';

const meta: Meta = {
  title: 'Basics/Typography',
  parameters: {
    layout: 'padded',
  },
};

export default meta;

export const Showcase: StoryObj = {
  render: () => (
    <div className="space-y-12 max-w-4xl">
      <section>
        <h2 className="text-2xl font-bold mb-8 border-b pb-2">Font Families</h2>
        <div className="space-y-6">
          <div data-font-family="default">
            <span className="text-xs opacity-50 uppercase block mb-1">
              Inclusive Sans (Default)
            </span>
            <p className="text-3xl font-sans font-bold">
              The quick brown fox jumps over the lazy dog
            </p>
          </div>
          <div data-font-family="dm-sans">
            <span className="text-xs opacity-50 uppercase block mb-1">
              DM Sans
            </span>
            <p className="text-3xl font-sans font-bold">
              The quick brown fox jumps over the lazy dog
            </p>
          </div>
          <div data-font-family="wix">
            <span className="text-xs opacity-50 uppercase block mb-1">
              Wix Madefor Text
            </span>
            <p className="text-3xl font-sans font-bold">
              The quick brown fox jumps over the lazy dog
            </p>
          </div>
          <div data-font-family="ar-one">
            <span className="text-xs opacity-50 uppercase block mb-1">
              AR One Sans
            </span>
            <p className="text-3xl font-sans font-bold">
              The quick brown fox jumps over the lazy dog
            </p>
          </div>
        </div>
      </section>

      <section>
        <h2 className="text-2xl font-bold mb-8 border-b pb-2">
          Typography Scale
        </h2>
        <div className="space-y-8">
          <div>
            <span className="text-[10px] font-mono opacity-40 uppercase block mb-1">
              text-5xl (48px)
            </span>
            <h1 className="text-5xl font-bold tracking-tight">
              Main Hero Heading
            </h1>
          </div>
          <div>
            <span className="text-[10px] font-mono opacity-40 uppercase block mb-1">
              text-4xl (36px)
            </span>
            <h2 className="text-4xl font-bold">Page Header Level 2</h2>
          </div>
          <div>
            <span className="text-[10px] font-mono opacity-40 uppercase block mb-1">
              text-3xl (30px)
            </span>
            <h3 className="text-3xl font-bold">Section Header Level 3</h3>
          </div>
          <div>
            <span className="text-[10px] font-mono opacity-40 uppercase block mb-1">
              text-2xl (24px)
            </span>
            <h4 className="text-2xl font-semibold">Sub-section Heading</h4>
          </div>
          <div>
            <span className="text-[10px] font-mono opacity-40 uppercase block mb-1">
              text-xl (20px)
            </span>
            <h5 className="text-xl font-semibold">Modal Title / Card Header</h5>
          </div>
          <div>
            <span className="text-[10px] font-mono opacity-40 uppercase block mb-1">
              text-lg (18px)
            </span>
            <p className="text-lg font-medium">
              Large body text or small header
            </p>
          </div>
          <div>
            <span className="text-[10px] font-mono opacity-40 uppercase block mb-1">
              text-base (16px / Default)
            </span>
            <p className="text-base">
              Standard body text for paragraphs and interface elements. It is
              designed to be highly legible at standard reading distances.
            </p>
          </div>
          <div>
            <span className="text-[10px] font-mono opacity-40 uppercase block mb-1">
              text-sm (14px)
            </span>
            <p className="text-sm">
              Secondary text, form labels, and table content. Used for less
              prominent information.
            </p>
          </div>
          <div>
            <span className="text-[10px] font-mono opacity-40 uppercase block mb-1">
              text-xs (12px)
            </span>
            <p className="text-xs">
              Captions, subtitles, breadcrumbs, and extra small badges.
            </p>
          </div>
        </div>
      </section>

      <section>
        <h2 className="text-2xl font-bold mb-8 border-b pb-2">Font Weights</h2>
        <div className="grid grid-cols-2 md:grid-cols-3 gap-6">
          <div className="font-light text-xl">Light (300)</div>
          <div className="font-normal text-xl">Normal (400)</div>
          <div className="font-medium text-xl">Medium (500)</div>
          <div className="font-semibold text-xl">Semibold (600)</div>
          <div className="font-bold text-xl">Bold (700)</div>
          <div className="font-black text-xl">Black (900)</div>
        </div>
      </section>

      <section>
        <h2 className="text-2xl font-bold mb-8 border-b pb-2">
          Styling Utilities
        </h2>
        <div className="space-y-4">
          <p className="italic">Italic text for emphasis</p>
          <p className="uppercase tracking-widest text-xs font-bold opacity-60">
            Uppercase with wider tracking
          </p>
          <p className="underline decoration-primary underline-offset-4">
            Underlined text with custom decoration
          </p>
          <p className="line-through opacity-40">
            Strikethrough text (deleted/inactive)
          </p>
          <p className="font-mono text-sm bg-base-300 px-2 py-1 rounded inline-block">
            Monospaced text for codes: 0xFAB22
          </p>
        </div>
      </section>
      <section>
        <h2 className="text-2xl font-bold mb-8 border-b pb-2">
          Markdown / Prose
        </h2>
        <div className="prose bg-base-100 p-6 rounded-xl border border-base-content/5">
          <h1>H1: Prose Heading</h1>
          <p>
            This is a paragraph inside a <code>.prose</code> container. It
            includes <strong>bold text</strong>,<em>italic text</em>, and a{' '}
            <a href="#">link to somewhere</a>.
          </p>
          <ul>
            <li>List item one</li>
            <li>
              List item two with a{' '}
              <code className="text-primary">code block</code>
            </li>
          </ul>
          <blockquote>
            "This is a blockquote showing standard quotation styling within the
            typography system."
          </blockquote>
        </div>
      </section>
    </div>
  ),
};
