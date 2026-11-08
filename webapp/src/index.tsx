// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import {setSiteUrl} from 'client';
import manifest from 'manifest';
import {docChanged, reducer} from 'plugin_state';
import type {DocChangedEvent} from 'plugin_state';
import React from 'react';
import type {Store} from 'redux';

import type {GlobalState} from '@mattermost/types/store';

import {getConfig} from 'mattermost-redux/selectors/entities/general';

import type {PluginRegistry} from 'types/mattermost-webapp';

import {DocIcon} from './components/common';
import DocCard from './components/doc_card';
import DocEditor from './components/doc_editor';
import DocList from './components/doc_list';
import {DocLinkRoute, makeFolderLinkRoute} from './components/link_routes';

import './styles.scss';

export default class Plugin {
    public async initialize(registry: PluginRegistry, store: Store<GlobalState>) {
        // @see https://developers.mattermost.com/extend/plugins/webapp/reference/
        setSiteUrl(getConfig(store.getState()).SiteURL || '');

        registry.registerReducer(reducer);

        const {showRHSPlugin, toggleRHSPlugin} = registry.registerRightHandSidebarComponent({
            component: DocList,
            title: 'Documents',
        });
        registry.registerChannelHeaderButtonAction({
            icon: <DocIcon/>,
            action: () => store.dispatch(toggleRHSPlugin),
            dropdownText: 'Documents',
            tooltipText: 'Channel documents',
        });

        registry.registerRootComponent(DocEditor);

        // Links: <site>/<team>/com.otules.mattermarkdown/doc/<id> and /folder/<id>. See links.ts.
        registry.registerNeedsTeamRoute('/doc', DocLinkRoute);
        registry.registerNeedsTeamRoute('/folder', makeFolderLinkRoute(showRHSPlugin));
        registry.registerPostTypeComponent('custom_mmdocs_doc', DocCard);

        registry.registerWebSocketEventHandler<Omit<DocChangedEvent, 'seq'>>(`custom_${manifest.id}_doc_changed`, (msg) => {
            store.dispatch(docChanged(msg.data));
        });

        // Events may have been missed while offline; an event with no document refreshes every list.
        registry.registerReconnectHandler(() => {
            store.dispatch(docChanged({doc_id: '', channel_id: '', action: 'updated'}));
        });
    }
}

declare global {
    interface Window {
        registerPlugin(pluginId: string, plugin: Plugin): void;
    }
}

window.registerPlugin(manifest.id, new Plugin());
