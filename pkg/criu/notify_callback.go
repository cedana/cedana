package criu

// Implements the Notify interface to support callbacks

import "github.com/rs/zerolog/log"

type NotifyCallback struct {
	PreDumpFunc             NotifyFunc
	PostDumpFunc            NotifyFunc
	PreRestoreFunc          NotifyFunc
	PostRestoreFunc         NotifyFuncPid
	NetworkLockFunc         NotifyFunc
	NetworkUnlockFunc       NotifyFunc
	SetupNamespacesFunc     NotifyFuncPid
	PostSetupNamespacesFunc NotifyFuncPid
	PreResumeFunc           NotifyFuncPid
	PostResumeFunc          NotifyFuncPid
	OrphanPtsMasterFunc     NotifyFuncFd
}

type (
	NotifyFunc    func() error
	NotifyFuncPid func(pid int32) error
	NotifyFuncFd  func(fd int32) error
)

func (n NotifyCallback) PreDump() error {
	if n.PreDumpFunc != nil {
		log.Debug().Msgf("executing predump anonymous function")
		err := n.PreDumpFunc()
		if err != nil {
			log.Error().Err(err).Msgf("failed to execute predump anonymous function")
			return err
		}
	}
	return nil
}

func (n NotifyCallback) PostDump() error {
	if n.PostDumpFunc != nil {
		log.Debug().Msgf("executing postdump anonymous function")
		err := n.PostDumpFunc()
		if err != nil {
			log.Error().Err(err).Msgf("failed to execute postdump anonymous function")
			return err
		}
	}
	return nil
}

func (n NotifyCallback) PreRestore() error {
	if n.PreRestoreFunc != nil {
		log.Debug().Msgf("executing prerestore anonymous function")
		err := n.PreRestoreFunc()
		if err != nil {
			log.Error().Err(err).Msgf("failed to execute prerestore anonymous function")
			return err
		}
	}
	return nil
}

func (n NotifyCallback) PreResume(pid int32) error {
	if n.PreResumeFunc != nil {
		log.Debug().Msgf("executing prerestore anonymous function")
		err := n.PreResumeFunc(pid)
		if err != nil {
			log.Error().Err(err).Msgf("failed to execute prerestore anonymous function")
			return err
		}
	}
	return nil
}

func (n NotifyCallback) PostRestore(pid int32) error {
	if n.PostRestoreFunc != nil {
		log.Debug().Msgf("executing postrestore anonymous function")
		err := n.PostRestoreFunc(pid)
		if err != nil {
			log.Error().Err(err).Msgf("failed to execute postrestore anonymous function")
			return err
		}
	}
	return nil
}

func (n NotifyCallback) NetworkLock() error {
	if n.NetworkLockFunc != nil {
		log.Debug().Msgf("executing networklock anonymous function")
		err := n.NetworkLockFunc()
		if err != nil {
			log.Error().Err(err).Msgf("failed to execute networklock anonymous function")
			return err
		}
	}
	return nil
}

func (n NotifyCallback) NetworkUnlock() error {
	if n.NetworkUnlockFunc != nil {
		log.Debug().Msgf("executing networkunlock anonymous function")
		err := n.NetworkUnlockFunc()
		if err != nil {
			log.Error().Err(err).Msgf("failed to execute networkunlock anonymous function")
			return err
		}
	}
	return nil
}

func (n NotifyCallback) SetupNamespaces(pid int32) error {
	if n.SetupNamespacesFunc != nil {
		log.Debug().Msgf("executing setupnamespaces anonymous function")
		err := n.SetupNamespacesFunc(pid)
		if err != nil {
			log.Error().Err(err).Msgf("failed to execute setupnamespaces anonymous function")
			return err
		}
	}
	return nil
}

func (n NotifyCallback) PostSetupNamespaces(pid int32) error {
	if n.PostSetupNamespacesFunc != nil {
		log.Debug().Msgf("executing postsetupnamespaces anonymous function")
		err := n.PostSetupNamespacesFunc(pid)
		if err != nil {
			log.Error().Err(err).Msgf("failed to execute postsetupnamespaces anonymous function")
			return err
		}
	}
	return nil
}

func (n NotifyCallback) PostResume(pid int32) error {
	if n.PostResumeFunc != nil {
		log.Debug().Msgf("executing postresume anonymous function")
		err := n.PostResumeFunc(pid)
		if err != nil {
			log.Error().Err(err).Msgf("failed to execute postresume anonymous function")
			return err
		}
	}
	return nil
}

func (n NotifyCallback) OrphanPtsMaster(fd int32) error {
	if n.OrphanPtsMasterFunc != nil {
		log.Debug().Msgf("executing orphanptsmaster anonymous function")
		err := n.OrphanPtsMasterFunc(fd)
		if err != nil {
			log.Error().Err(err).Msgf("failed to execute orphanptsmaster anonymous function")
			return err
		}
	}
	return nil
}
