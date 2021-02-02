package apps

import (
	"context"
	"reagent/common"
	"reagent/container"
	"reagent/logging"
)

func (sm *StateMachine) publishApp(payload common.TransitionPayload, app *common.App) error {
	// don't set release state to exists until it is published
	app.ReleaseBuild = true
	err := sm.buildDevApp(payload, app)
	if err != nil {
		return err
	}

	ctx := context.Background()
	err = sm.Container.Tag(ctx, payload.ImageName.Dev, payload.NewImageName)
	if err != nil {
		return err
	}

	err = sm.setState(app, common.PUBLISHING)
	if err != nil {
		return err
	}

	authConfig := container.AuthConfig{
		Username: payload.RegisteryToken,
		Password: sm.Container.GetConfig().ReswarmConfig.Secret,
	}

	reader, err := sm.Container.Push(ctx, payload.NewImageName, authConfig)
	if err != nil {
		return err
	}

	err = sm.LogManager.Stream(payload.PublishContainerName, logging.PUSH, reader)
	if err != nil {
		return err
	}

	err = sm.setState(app, common.PRESENT)
	if err != nil {
		return err
	}

	return nil
}

// info.stage = 'PROD'
// info.state = 'PRESENT'
// info.readme = readme
// info.exists = true
// info.git_hash = gitHash
// info.build_message = 'successBuild'
// const [newrel] = await studio.create_release([info], kwargs, details)
// params.session.publish(topic, [{type: 'build', chunk: '#################### Image pushed successfully ####################'}]) // publish to frontend console
// info.stage = 'DEV'

// console.log('[containers] App published successfully')
// params.session.publish(`reswarm.app.list/onupdate`, [
//     {
//         app_key: info.app_key,
//         version: info.version,
//         release_key: newrel.release_key,
//         newest_version: info.version
//     }
// ])

// let res_data = await devices.read_device_to_app([info], kwargs, details)
// console.log('[containers] publish to app in device lists (DEV)', res_data)

// params.session.publish(
//     `reswarm.devices.device_to_app.${info.swarm_key}.list/onupdate`,
//     res_data
// )

// let res_data_prod = await devices.read_device_to_app(
//     [{ ...info, stage: 'PROD' }],
//     kwargs,
//     details
// )

// console.log('[containers] publish to app in device lists (PROD)', res_data_prod)
// // update prod container in dta
// params.session.publish(`reswarm.devices.device_to_app.${info.swarm_key}.list/onupdate`, res_data_prod)
//     })
//     .catch((error) => {
//         info.state = 'FAILED';
//         extras.handle_errors(error, 'Failed to create release in database')
//     })
//     .finally(async () => {
//         console.log('[containers ] finally virtually removing from device')
//         info.manually_requested_state = 'PRESENT'
//         info.stage = 'DEV'
//         await update_app_on_device([info], kwargs, details)
//         try {
//             await fsPromises.rmdir(tmpReleaseFolder, { recursive: true });
//         } catch (error) {
//             extras.handle_errors(error)
//         }
//     })
// .catch((error) => {
//     const message = error?.args?.[0];
//     if (message?.includes('requested access to the resource is denied')) {
//         info.build_message = 'errorInsufficientPrivsForRelease'
//         studio.update_release([info], kwargs, details)
//         throw new Error('errorInsufficientPrivsForRelease')
//     }
//     info.build_message = 'errorPublish'
//     studio.update_release([info], kwargs, details)
//     params.session.publish(topic, [{type: 'build', chunk: '#################### failed to push Image ####################'}]) // publish to frontend console

//     throw error
// })
// )
