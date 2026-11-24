import type { SubMenuItem, MenuGroup, ModuleMenuConfig } from '@code/common';
import { MODULE_KEY, MODULE_TITLE } from './constants';

export type { SubMenuItem, MenuGroup, ModuleMenuConfig };

export const markdocxMenuConfig: ModuleMenuConfig = {
  moduleKey: MODULE_KEY,
  moduleName: MODULE_TITLE,
  groups: [
    {
      title: '文档转换',
      items: [
        {
          path: '/workbench',
          label: '转换与审阅工作台',
          headerTitle: 'Word 转 Markdown · 智能转译与图文对照审阅',
          icon: 'M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z',
        },
        {
          path: '/jobs',
          label: '本次会话任务',
          headerTitle: '本次会话任务列表',
          icon: 'M4 6h16M4 10h16M4 14h16M4 18h16',
        },
      ],
    },
  ],
};

export const menuGroups: MenuGroup[] = markdocxMenuConfig.groups;
export const menuItems: SubMenuItem[] = markdocxMenuConfig.groups.flatMap((g) => g.items);

type MenuChangeListener = (config: ModuleMenuConfig) => void;
const listeners = new Set<MenuChangeListener>();

export function subscribeMenuChanges(listener: MenuChangeListener): () => void {
  listeners.add(listener);
  listener(markdocxMenuConfig);
  return () => {
    listeners.delete(listener);
  };
}

export default markdocxMenuConfig;
