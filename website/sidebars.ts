import type {SidebarsConfig} from '@docusaurus/plugin-content-docs';

const sidebars: SidebarsConfig = {
  docs: [
    'intro',
    'quickstart',
    {
      type: 'category',
      label: 'Concepts',
      collapsed: false,
      items: [
        'concepts/autonomous-loop',
        'concepts/codegen-agents',
        'concepts/cards-and-repos',
      ],
    },
    'self-hosting',
    'architecture',
    'mobile',
    'contributing',
  ],
};

export default sidebars;
