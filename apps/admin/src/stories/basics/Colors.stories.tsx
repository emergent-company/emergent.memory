import type { Meta, StoryObj } from '@storybook/react';
import React from 'react';

const meta: Meta = {
  title: 'Basics/Colors',
  parameters: {
    layout: 'padded',
  },
};

export default meta;

const ColorSwatch = ({
  name,
  className,
  variable,
}: {
  name: string;
  className: string;
  variable: string;
}) => (
  <div className="flex flex-col gap-2">
    <div
      className={`h-24 w-full rounded-xl border border-base-content/10 shadow-sm ${className}`}
    ></div>
    <div className="flex flex-col">
      <span className="font-bold text-sm uppercase tracking-wider">{name}</span>
      <code className="text-xs opacity-50">{variable}</code>
    </div>
  </div>
);

export const Palette: StoryObj = {
  render: () => (
    <div className="space-y-12">
      <section>
        <h2 className="text-2xl font-bold mb-6">Brand Colors</h2>
        <div className="grid grid-cols-2 md:grid-cols-4 gap-6">
          <ColorSwatch
            name="Primary"
            className="bg-primary"
            variable="--color-primary"
          />
          <ColorSwatch
            name="Secondary"
            className="bg-secondary"
            variable="--color-secondary"
          />
          <ColorSwatch
            name="Accent"
            className="bg-accent"
            variable="--color-accent"
          />
          <ColorSwatch
            name="Neutral"
            className="bg-neutral"
            variable="--color-neutral"
          />
        </div>
      </section>

      <section>
        <h2 className="text-2xl font-bold mb-6">Base Colors</h2>
        <div className="grid grid-cols-2 md:grid-cols-4 gap-6">
          <ColorSwatch
            name="Base 100"
            className="bg-base-100"
            variable="--color-base-100"
          />
          <ColorSwatch
            name="Base 200"
            className="bg-base-200"
            variable="--color-base-200"
          />
          <ColorSwatch
            name="Base 300"
            className="bg-base-300"
            variable="--color-base-300"
          />
          <ColorSwatch
            name="Base Content"
            className="bg-base-content"
            variable="--color-base-content"
          />
        </div>
      </section>

      <section>
        <h2 className="text-2xl font-bold mb-6">State Colors</h2>
        <div className="grid grid-cols-2 md:grid-cols-4 gap-6">
          <ColorSwatch
            name="Info"
            className="bg-info text-info-content"
            variable="--color-info"
          />
          <ColorSwatch
            name="Success"
            className="bg-success text-success-content"
            variable="--color-success"
          />
          <ColorSwatch
            name="Warning"
            className="bg-warning text-warning-content"
            variable="--color-warning"
          />
          <ColorSwatch
            name="Error"
            className="bg-error text-error-content"
            variable="--color-error"
          />
        </div>
      </section>

      <section>
        <h2 className="text-2xl font-bold mb-6">Text Colors</h2>
        <div className="space-y-4">
          <div className="flex items-center gap-4">
            <div className="w-12 h-6 rounded bg-base-content"></div>
            <span className="text-base-content font-medium">
              Standard Text (base-content)
            </span>
          </div>
          <div className="flex items-center gap-4">
            <div className="w-12 h-6 rounded bg-base-content/60"></div>
            <span className="text-base-content/60 font-medium">
              Secondary Text (60% opacity)
            </span>
          </div>
          <div className="flex items-center gap-4">
            <div className="w-12 h-6 rounded bg-base-content/40"></div>
            <span className="text-base-content/40 font-medium">
              Disabled Text (40% opacity)
            </span>
          </div>
        </div>
      </section>
    </div>
  ),
};
