import { useState } from 'react'

import { useMutation } from '@sourcegraph/http-client'
import { ErrorAlert, PageHeader } from '@sourcegraph/wildcard'

import { FilteredConnectionFilterValue } from '../../../components/FilteredConnection'
import {
    UpdatePackageRepoFilterVariables,
    PackageMatchBehaviour,
    PackageRepoFilterFields,
    UpdatePackageRepoFilterResult,
} from '../../../graphql-operations'
import { updatePackageRepoFilterMutation } from '../backend'
import { BehaviourSelect } from '../components/BehaviourSelect'
import { MultiPackageForm, MultiPackageState } from '../components/MultiPackageForm'
import { SinglePackageForm, SinglePackageState } from '../components/SinglePackageForm'

import { BlockType } from './AddPackageFilterModalContent'

import styles from './AddPackageFilterModalContent.module.scss'

const getInitialState = (packageFilter: PackageRepoFilterFields): SinglePackageState | MultiPackageState => {
    const nameGlob = packageFilter.nameFilter?.packageGlob || ''

    if (nameGlob !== '') {
        return {
            ecosystem: packageFilter.kind,
            nameFilter: nameGlob,
        }
    }

    if (packageFilter.versionFilter) {
        return {
            ecosystem: packageFilter.kind,
            name: packageFilter.versionFilter.packageName,
            versionFilter: packageFilter.versionFilter.versionGlob,
        }
    }

    throw new Error(`Unable to find filter for package filter ${packageFilter.id}`)
}

export interface EditPackageFilterModalContentProps {
    packageFilter: PackageRepoFilterFields
    filters: FilteredConnectionFilterValue[]
    onDismiss: () => void
}

export const EditPackageFilterModalContent: React.FunctionComponent<EditPackageFilterModalContentProps> = ({
    packageFilter,
    filters,
    onDismiss,
}) => {
    const [behaviour, setBehaviour] = useState<PackageMatchBehaviour>(packageFilter.behaviour)
    const initialState = getInitialState(packageFilter)
    const [blockType, setBlockType] = useState<BlockType>('name' in initialState ? 'single' : 'multiple')

    const [updatePackageRepoFilter, { error }] = useMutation<
        UpdatePackageRepoFilterResult,
        UpdatePackageRepoFilterVariables
    >(updatePackageRepoFilterMutation, { onCompleted: onDismiss })

    return (
        <>
            <PageHeader path={[{ text: 'Edit package filter' }]} headingElement="h2" className={styles.header} />
            <div className={styles.content}>
                <BehaviourSelect value={behaviour} onChange={setBehaviour} />
                {blockType === 'single' ? (
                    <SinglePackageForm
                        initialState={initialState as SinglePackageState}
                        filters={filters}
                        setType={setBlockType}
                        onDismiss={onDismiss}
                        onSave={blockState =>
                            updatePackageRepoFilter({
                                variables: {
                                    behaviour,
                                    id: packageFilter.id,
                                    kind: blockState.ecosystem,
                                    filter: {
                                        versionFilter: {
                                            packageName: blockState.name,
                                            versionGlob: blockState.versionFilter,
                                        },
                                    },
                                },
                            })
                        }
                    />
                ) : (
                    <MultiPackageForm
                        initialState={initialState as MultiPackageState}
                        filters={filters}
                        setType={setBlockType}
                        onDismiss={onDismiss}
                        onSave={blockState =>
                            updatePackageRepoFilter({
                                variables: {
                                    behaviour,
                                    id: packageFilter.id,
                                    kind: blockState.ecosystem,
                                    filter: {
                                        nameFilter: {
                                            packageGlob: blockState.nameFilter,
                                        },
                                    },
                                },
                            })
                        }
                    />
                )}
                {error && <ErrorAlert error={error} />}
            </div>
        </>
    )
}
